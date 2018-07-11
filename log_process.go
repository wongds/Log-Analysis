package main

import (
	"time"
	"strings"
	"fmt"
	"os"
	"bufio"
	"io"
	"path/filepath"
	"regexp"
	"log"
	"strconv"
	"net/url"
)

type Write interface {
	write(wChan chan *Message)
}

type Read interface {
	read(rChan chan []byte)
}

//结构体作为对象的封装
type LogProcess struct {
	r Read
	w Write

	rCh chan []byte // goroutine之间通信的通道，同步数据
	wCh chan *Message
	//path         string // 读取文件的路径
	//influxdbinfo string // influx data source //类似于用户名密码ip等的信息
}

// 优化增加内容，包含读取文件需要的信息的结构
type ReadFromFile struct {
	path string
}

// 写到influxdb，包含influxdb的信息的结构
type WriteToInfluxdb struct {
	influxdbinfo string
}

// 接受日志解析内容的结构体
type Message struct {
	TimeLocal                    time.Time
	BytesSent                    int
	Path, Method, Scheme, Status string
	UpstreamTime, RequestTime    float64
}

// 优化：只有这三个函数扩展性很差，只能从文件读写，可能还需要从标准输入输出里面读写，再定义模块太麻烦了，所以需要实现读写的接口，
// 读取模块
func (rf *ReadFromFile) read(rCh chan []byte) {
	// 打开文件，还有一种读取文件的方法是	ioutil.ReadFile(rf.path)。但是这种没有os.open的方式灵活，os.open的方式能够流式读取文件最后面的
	f, err := os.Open(rf.path)
	if err != nil {
		// 打印当前目录，但是好像事实上是运行时目录，不是project目录
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

/*
正则表达式
([\d\.]+)\s+([^ \[]+)\s+([^ \[]+)\s+\[([^\]]+)\]\s+([a-z]+)\s+\"([^"]+)\"\s+(\d{3})\s+(\d+)\s+\"([^"]+)\"\s+\"(.*?)\"\s+\"([\d\.-]+)\"\s+([\d\.-]+)\s+([\d\.-]+)
需要匹配的内容示例
172.0.0.12 - - [04/Mar/2018:13:49:52 +0000] http "GET /foo?queery=t HTTP/1.0" 200 2133 "-" "KeepAliveClient" "-" 1.005 1.854
注意： 测试的时候出现了一点问题，就是echo追加内容到文件的时候，会解析带""的字符串去掉""，所以需要给”的地方加上转义字符\"
*/
// 解析模块
func (l *LogProcess) ParseFromRead() {
	reger := regexp.MustCompile(`([\d\.]+)\s+([^ \[]+)\s+([^ \[]+)\s+\[([^\]]+)\]\s+([a-z]+)\s+\"([^"]+)\"\s+(\d{3})\s+(\d+)\s+\"([^"]+)\"\s+\"(.*?)\"\s+\"([\d\.-]+)\"\s+([\d\.-]+)\s+([\d\.-]+)`)
	// 时区
	local, _ := time.LoadLocation("Asia/Shanghai")

	// 循环解析每一行的内容
	for line := range l.rCh {
		ret := reger.FindStringSubmatch(string(line))
		if len(ret) != 14 {
			fmt.Println("长度是：", len(ret))
			log.Println("匹配失败：", string(line))
			continue
		}
		// 创建接受对象实例
		message := &Message{}
		// 将时间格式化golang中的时间格式，时间字段是在第四个字段里面
		time, err := time.ParseInLocation("02/Jan/2006:15:04:05 +0000", ret[4], local)
		if err != nil {
			log.Println("时间格式不对", err.Error(), ret[4])
			continue
		}
		message.TimeLocal =time

		byteSent, _ := strconv.Atoi(ret[8])
		message.BytesSent = byteSent

		reqLine := strings.Split(ret[6], " ")
		if len(reqLine) != 3 {
			log.Println("请求行解析失败：", ret[6])
			continue
		}
		message.Method = reqLine[0]
		url, err := url.Parse(reqLine[1])
		if err != nil {
			log.Println("path解析失败", err.Error(), reqLine[1])
			continue
		}
		message.Path = url.Path

		message.Scheme = ret[5]
		message.Status = ret[7]

		upstreamTime, _ := strconv.ParseFloat(ret[12], 64)
		requestTime, _ := strconv.ParseFloat(ret[13], 64)
		message.UpstreamTime = upstreamTime
		message.RequestTime = requestTime


		l.wCh <- message
	}
}

// 写入模块
func (l *WriteToInfluxdb) write(wCh chan *Message) {
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
			path: "./log.l",
		},
		w: &WriteToInfluxdb{
			influxdbinfo: "root&passworld&ip",
		},
		rCh: make(chan []byte),
		wCh: make(chan *Message),
		//path:         "/var/log/messages/",
		//influxdbinfo: "passworld&root",
	}

	go loger.r.read(loger.rCh)
	go loger.ParseFromRead()
	go loger.w.write(loger.wCh)

	time.Sleep(60 * time.Second)
}
