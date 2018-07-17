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
	"github.com/influxdata/influxdb/client/v2"
	"flag"
)

type Write interface {
	write(wChan chan *Message)
}

type Read interface {
	read(rChan chan []byte)
}

// 结构体作为对象封装
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
	TimeLocal                    time.Time // 当前时间
	BytesSent                    int       // 发送大小
	Path, Method, Scheme, Status string    // 路径，调用的方法，scheme， 状态
	UpstreamTime, RequestTime    float64   // 上传时间，请求时间
}

// 优化：只有这三个函数扩展性很差，只能从文件读写，可能还需要从标准输入输出里面读写，再定义模块太麻烦了，所以需要实现读写的接口，
// 读取模块，从文件中读取内容写入到通道中
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
// 解析模块，循环解析通道中的内容
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
		message.TimeLocal = time

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
// 将解析到的内容写入到时序数据库influxdb
func (lw *WriteToInfluxdb) write(wCh chan *Message) {
	// Create a new HTTPClient
	lwslice := strings.Split(lw.influxdbinfo, "&")
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     lwslice[0],
		Username: lwslice[1],
		Password: lwslice[2],
	})
	if err != nil {
		log.Fatalf("读取写入信息失败,%s", err.Error())
	}
	defer c.Close()

	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  lwslice[3],
		Precision: lwslice[4],
	})
	if err != nil {
		log.Fatal(err)
	}

	// 循环写入数据
	for value := range wCh {
		// Create a point and add to batch
		// Path, Method, Scheme, Status string
		tags := map[string]string{"path": value.Path, "method": value.Method, "scheme": value.Scheme, "status": value.Status}
		// UpstreamTime, RequestTime    float64
		//TimeLocal                    time.Time // 当前时间
		//BytesSent                    int // 发送大小
		fields := map[string]interface{}{
			"time":         value.TimeLocal,
			"bytesent":     value.BytesSent,
			"upstreamtime": value.UpstreamTime,
			"requesttime":  value.RequestTime,
		}

		pt, err := client.NewPoint("nginx_log", tags, fields, value.TimeLocal)
		if err != nil {
			log.Fatal(err)
		}
		bp.AddPoint(pt)

		// Write the batch
		if err := c.Write(bp); err != nil {
			log.Fatal(err)
		}
		// Close client resources
		if err := c.Close(); err != nil {
			log.Fatal(err)
		}
		log.Println("写入成功")
	}
}

func main() {
	var path, influxdbinfo string
	flag.StringVar(&path, "path", "./log.txt", "这里应该是命令行参数-path=\"日志文件的地址（反斜杠是转义字符，实际上不需要加）\"")
	flag.StringVar(&influxdbinfo, "influxdbinfo", "http://192.168.75.144:8086&admin&admin&MYDB&s", "命令行参数，数据库信息-influxdb=\"数据库信息字符串\"")
	flag.Parse()
	loger := &LogProcess{
		// 这里发现一定要使用&struct，不知道为什么。难道是直接实例化不会当做接口实现，一定要传引用？
		r: &ReadFromFile{
			//path: "D:/mygo/src/Log-Analysis/log.txt",
			path: path,
		},
		w: &WriteToInfluxdb{
			// 这里这样写死，扩展性很差。可以直接写入到启动参数
			influxdbinfo: influxdbinfo,
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
