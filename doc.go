package callengine
/*
    本PKG提供函数注册调度执行引擎。
引擎由 执行生产线（PipLine）和引擎主体组成。

用法：call
1） 创建引擎
  newEngine
参数1：引擎规模（并发goroutine数）
参数2：生产线参数数组
	生产线参数
         1 生产线名称（用于取得生产线）
         2 生产线buffer大小
         3 生产线权重（暂时未使用）

例：
   var ppa []PipLinePara
	ppa = append(ppa, PipLinePara{
		"test1",
		1000,
		1,
	})
    e := newEngine(125, ppa)

2) 注册函数
   RegFun
参数1：函数名称（标记注册函数）
参数2：函数名（实现的函数实体名）

例：
    e.RegFun("add", add)

    函数实现：
	func add(i, j int) int {
		return i + j
	}

3) 开启/停止引擎
   Start()/Stop()

4) 取得生产线
   GetPipLine
参数： 生产线名称

例：
	p1 := e.GetPipLine("test1")

注意：以上函数为 Engine对象的函数

5） 执行已注册函数
执行函数必须在选定生产线上执行。生产线提供多种函数执行模式。
	SyncExec
	AsyncExec
	Exec
参数 ：
    超时时间：
		0 ：永远不超时
		>0 :超时后返回超时err（注册函数有可能被执行也有可能没被执行）
	同步flg ：
		true 同步执行
		false 异步执行 （异步执行得不到注册函数执行的返回值）
		（调用SyncExec，AsyncExec时无此参数）
	函数名称：
		注册到引擎的函数名称
	已注册函数参数：
		参数个数不定，支持任意类型

例：
	res, err := p1.Exec(time.Second, true, "add", 1, 2)
	返回值 res 为 已注册"add"函数返回值的数组（可返回多个值）
	返回值 err 为 Exec 执行时的Error

*/