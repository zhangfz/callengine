package callengine

import (
	"sync"
	"reflect"
	"sync/atomic"
	"errors"
	"context"
	"time"
	"fmt"
)

const (
	DefaultConcurrentNum = 256
	MaxconcurrentNum     = DefaultConcurrentNum * 100
)

type Engine struct {
	frwlock       sync.RWMutex                  //方法map读写锁
	sslock        sync.Mutex                    //启动停止锁
	funmap        map[interface{}]reflect.Value //注册方法map
	piplines      map[interface{}]*Pipline      //缓冲管道map
	concurrentNum int
	cmdCha        chan *CallCMD
	isStart       bool
}

type CallCMD struct {
	Context context.Context
	Op      interface{}
	InPara  []interface{}
	RetCha  chan []interface{}
}

type PipLinePara struct {
	Name   interface{}
	Size   int32
	Weight int32
}

type Pipline struct {
	name   interface{}
	e      *Engine
	used   int32
	size   int32
	weight int32
	isOpen bool
	cha    chan *CallCMD
}

func newPipline(para PipLinePara, e *Engine) *Pipline {
	return &Pipline{
		name:   para.Name,
		e:      e,
		used:   0,
		size:   para.Size,
		weight: para.Weight,
		cha:    make(chan *CallCMD, para.Size),
	}
}

func (p *Pipline) SyncExec(timeoute time.Duration, opName interface{}, args ... interface{}) (ret []interface{}, execerr error) {
	ret, execerr = p.Exec(timeoute, true, opName, args...)
	return
}

func (p *Pipline) AsyncExec(opName interface{}, args ... interface{}) (execerr error) {
	_, execerr = p.Exec(0, false, opName, args...)
	return
}

func (p *Pipline) Exec(timeoute time.Duration, sync bool, opName interface{}, args ... interface{}) (ret []interface{}, execerr error) {
	if ! p.isOpen {
		execerr = fmt.Errorf("Pipline [%v] is Closed." , p.name)
		return
	}
	execerr = p.checkOp(opName, args...)
	if execerr != nil {
		return
	}
	cont := context.Background()
	if timeoute > 0 {
		cont, _ = context.WithTimeout(cont, timeoute)
	}
	var retcha chan []interface{}
	if sync {
		retcha = make(chan []interface{})
	}
	execcmd := &CallCMD{
		Context: cont,
		Op:      opName,
		InPara:  args,
		RetCha:  retcha,
	}
	atomic.AddInt32(&p.used, int32(1))
	p.cha <- execcmd

	if sync {
		select {
		case <-cont.Done():
			execerr = fmt.Errorf("Exec [ %v ] timeout.", opName)
		case ret = <-retcha:
		}
	}
	return
}

func (p *Pipline) checkOp(opName interface{}, args ... interface{}) error {
	return p.e.checkOp(opName, args...)
}

func (p *Pipline) FreeSize() int32 {
	frees := p.size - p.used
	if frees > 0 {
		return frees
	}
	return 0
}

func (p *Pipline) IsEmpty() bool {
	return p.used == 0
}

func (e *Engine) checkOp(opName interface{}, args ... interface{}) error {
	e.frwlock.RLock()
	fn, ok := e.funmap[opName]
	e.frwlock.RUnlock()
	if !ok {
		return fmt.Errorf("func [ %v ] is not registered. ", opName)
	}
	argl := len(args)
	if argl != fn.Type().NumIn() {
		return errors.New("The number of args is not adapted.")
	}
	for k, arg := range args {
		argtype := fn.Type().In(k)
		if reflect.TypeOf(arg) != argtype {
			return errors.New("Parameter type mismatch!")
		}

	}
	return nil
}

func (e *Engine) RegFun(name interface{}, f interface{}) (err error) {

	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		err = errors.New("Not func type, can not be registered！")
		return
	}
	e.frwlock.Lock()
	if _, ok := e.funmap[name]; ok {
		err = errors.New("Multiple registration！")
		e.frwlock.Unlock()
		return
	}
	e.funmap[name] = v;
	e.frwlock.Unlock()
	return
}

func (e *Engine) Start() {
	e.sslock.Lock()
	defer e.sslock.Unlock()
	if e.isStart {
		return
	}
	defer func() {
		e.isStart = true
	}()

	e.cmdCha = make(chan *CallCMD)

	for i := 0; i < e.concurrentNum; i++ {
		go func() {
			for cmd := range e.cmdCha {
				select {
				case <-cmd.Context.Done():
					continue
				default:
					err, res := e.call(cmd.Op, cmd.InPara...)
					if err != nil {
						fmt.Println(err.Error())
						continue
					}
					if cmd.RetCha != nil {
						var ret []interface{}
						for _, v := range res {
							ret = append(ret, v.Interface())
						}
						select {
						case <-cmd.Context.Done():
							continue
						case cmd.RetCha <- ret:
						}
					}

				}
			}
		}()
	}

	for k, v := range e.piplines {
		go func(name interface{}, pip *Pipline) {
			pip.isOpen = true
			defer func() { pip.isOpen = false }()
			for cmd := range pip.cha {
				e.cmdCha <- cmd
				atomic.AddInt32(&pip.used, int32(-1))
			}
		}(k, v)
	}
}

func (e *Engine) Stop(timeout time.Duration) {
	e.sslock.Lock()
	defer e.sslock.Unlock()
	if !e.isStart {
		return
	}

	e.isStart = false

	for _, v := range e.piplines {
		close(v.cha)
	}

	for {
		select {
		case <-time.After(timeout):
			goto close
		default:
			empty := true
			for _, v := range e.piplines {
				if !v.IsEmpty() {
					fmt.Println(v.FreeSize())
					empty = false
				}
			}
			if empty {
				goto close
			}
			time.Sleep(time.Millisecond)
		}
	}
close:
	close(e.cmdCha)
}

func (e *Engine) GetPipLine(name interface{}) (pip *Pipline) {

	if pipl, ok := e.piplines[name]; ok {
		pip = pipl
	}
	return
}

func newEngine(poolSize int, piplines []PipLinePara) *Engine {
	if poolSize == 0 {
		poolSize = DefaultConcurrentNum
	}
	if poolSize > MaxconcurrentNum {
		poolSize = MaxconcurrentNum
	}
	eng := &Engine{
		funmap:        make(map[interface{}]reflect.Value),
		piplines:      make(map[interface{}]*Pipline),
		concurrentNum: poolSize,
	}

	for _, pip := range piplines {
		pipl := newPipline(pip, eng)
		eng.piplines[pip.Name] = pipl
	}

	return eng
}

func (e *Engine) call(name interface{}, args ... interface{}) (callerr error, ret []reflect.Value) {
	e.frwlock.RLock()
	defer e.frwlock.RUnlock()
	fn, ok := e.funmap[name]
	if !ok {
		callerr = fmt.Errorf("%s does not exist.", name)
		return
	}
	if len(args) != fn.Type().NumIn() {
		callerr = errors.New("The number of args is not adapted.")
		return
	}
	in := make([]reflect.Value, len(args))
	for k, arg := range args {
		argtype := fn.Type().In(k)
		if reflect.TypeOf(arg) != argtype {
			callerr = errors.New("Parameter type mismatch!")
			return
		}
		in[k] = reflect.ValueOf(arg)
	}
	ret = fn.Call(in)
	return
}
