package registry

import (
	"errors"
	"go/ast"
	"log"
	"reflect"
	"strings"
	"sync/atomic"
)

type MethodEntry struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

func (m *MethodEntry) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *MethodEntry) NewArgv() reflect.Value {
	var argv reflect.Value
	// arg may be a pointer type, or a value type
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *MethodEntry) NewReplyv() reflect.Value {
	// reply must be a pointer type
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

type Service struct {
	name       string
	typ        reflect.Type
	serviceObj reflect.Value
	method     map[string]*MethodEntry
}

func newService(serviceObj interface{}) *Service {
	service := new(Service)
	service.serviceObj = reflect.ValueOf(serviceObj)
	service.name = reflect.Indirect(service.serviceObj).Type().Name()
	service.typ = reflect.TypeOf(serviceObj)
	if !ast.IsExported(service.name) {
		log.Fatalf("rpc server: %s is not a valid service name", service.name)
	}
	service.registerMethods()
	return service
}

func (service *Service) registerMethods() {
	service.method = make(map[string]*MethodEntry)
	for i := 0; i < service.typ.NumMethod(); i++ {
		method := service.typ.Method(i)
		mType := method.Type
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		service.method[method.Name] = &MethodEntry{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", service.name, method.Name)
	}
}

func (service *Service) Call(m *MethodEntry, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	function := m.method.Func
	returnValues := function.Call([]reflect.Value{service.serviceObj, argv, replyv})
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}

func isExportedOrBuiltinType(typ reflect.Type) bool {
	return ast.IsExported(typ.Name()) || typ.PkgPath() == ""
}

type Registry struct {
	serviceMap map[string]*Service
}

func NewRegistry() *Registry {
	return &Registry{
		serviceMap: make(map[string]*Service),
	}
}

var DefaultRegistry = NewRegistry()

// 注册服务
func (registry *Registry) Register(serviceObj interface{}) error {
	if registry.serviceMap == nil {
		registry.serviceMap = make(map[string]*Service)
	}

	service := newService(serviceObj)
	if _, exists := registry.serviceMap[service.name]; exists {
		return errors.New("registry: service already defined: " + service.name)
	}
	registry.serviceMap[service.name] = service
	return nil
}

func (registry *Registry) FindService(serviceMethod string) (*Service, *MethodEntry, error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		return nil, nil, errors.New("registry: service/method request ill-formed: " + serviceMethod)
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	storedService, ok := registry.serviceMap[serviceName]
	if !ok {
		return nil, nil, errors.New("registry: can't find service " + serviceName)
	}
	mtype, ok := storedService.method[methodName]
	if !ok {
		return nil, nil, errors.New("registry: can't find method " + methodName)
	}
	return storedService, mtype, nil
}
