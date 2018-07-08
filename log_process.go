package main

import (
	"time"
	"strings"
	"fmt"
)

type Write interface {
	write(wChan chan string)
}

type Read interface {
	read(rChan chan string)
}

//结构体作为对象的封装
type LogProcess struct {
	r Read
	w Write

	rCh chan string // goroutine之间通信的通道，同步数据
	wCh chan string
	//path         string // 读取文件的路径
	//influxdbinfo string // influx data source //类似于用户名密码ip等的信息
}

// 优化增加内容
type ReadFromFile struct {
	path string
}

type WriteToInfluxdb struct {
	influxdbinfo string
}

// 优化：只有这三个函数扩展性很差，只能从文件读写，可能还需要从标准输入输出里面读写，再定义模块太麻烦了，所以需要实现读写的接口，
// 读取模块
func (rf *ReadFromFile) read(rCh chan string) {
	str := "message"
	rCh <- str
}

// 解析模块
func (l *LogProcess) ParseFromRead() {
	str := <-l.rCh
	str = strings.ToUpper(str)
	l.wCh <- str
}

// 写入模块
func (l *WriteToInfluxdb) write(wCh chan string) {
	fmt.Println(<-wCh)
}

func main() {
	loger := &LogProcess{
		// 这里发现一定要使用&struct，不知道为什么。难道是直接实例化不会当做接口实现，一定要传引用？
		r: &ReadFromFile{
			path: "/var/log/messages/",
		},
		w: &WriteToInfluxdb{
			influxdbinfo: "root&passworld&ip",
		},
		rCh:  make(chan string),
		wCh: make(chan string),
		//path:         "/var/log/messages/",
		//influxdbinfo: "passworld&root",
	}

	go loger.r.read(loger.rCh)
	go loger.ParseFromRead()
	go loger.w.write(loger.wCh)

	time.Sleep(1 * time.Second)
}
