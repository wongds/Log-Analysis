package main

import (
	"time"
	"strings"
	"fmt"
	"os"
	"bufio"
	"io"
	"path/filepath"
)

type Write interface {
	write(wChan chan string)
}

type Read interface {
	read(rChan chan []byte)
}

//结构体作为对象的封装
type LogProcess struct {
	r Read
	w Write

	rCh chan []byte // goroutine之间通信的通道，同步数据
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
func (rf *ReadFromFile) read(rCh chan []byte) {
	// 打开文件
	f, err := os.Open(rf.path)
	if err != nil {
		dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
		fmt.Printf("ERROR: 在当前目录：%s，打开文件出错%s", dir, err.Error())
	}
	// 从文件末尾开始逐行读取数据
	f.Seek(0, 2)
	// 这样可以逐行读取，并且效率比较高，因为会创建更大的buffer
	rd := bufio.NewReader(f)
	// 循环读取
	for {
		line, err := rd.ReadBytes('\n')
		if err == io.EOF {
			time.Sleep(500 * time.Millisecond)
			continue
		} else if err != nil {
			panic(fmt.Sprintf("PANIC: 文件读取失败，%s", err.Error()))
		}
		rCh <- line[:len(line)-1]
	}
	//str := "message"
	//rCh <- str
}

// 解析模块
func (l *LogProcess) ParseFromRead() {
	// 循环解析
	for str := range l.rCh {
		l.wCh <- strings.ToUpper(string(str))
	}
}

// 写入模块
func (l *WriteToInfluxdb) write(wCh chan string) {
	// 循环写入
	for value := range wCh {
		fmt.Println(value)
	}
}

func main() {
	loger := &LogProcess{
		// 这里发现一定要使用&struct，不知道为什么。难道是直接实例化不会当做接口实现，一定要传引用？
		r: &ReadFromFile{
			//path: "D:/mygo/src/Log-Analysis/log.txt",
			path: "./log.txt",
		},
		w: &WriteToInfluxdb{
			influxdbinfo: "root&passworld&ip",
		},
		rCh: make(chan []byte),
		wCh: make(chan string),
		//path:         "/var/log/messages/",
		//influxdbinfo: "passworld&root",
	}

	go loger.r.read(loger.rCh)
	go loger.ParseFromRead()
	go loger.w.write(loger.wCh)

	time.Sleep(60 * time.Second)
}
