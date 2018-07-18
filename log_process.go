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
	"net/http"
	"encoding/json"
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
			TypeMointorChan <- TypeErrNum
			panic(fmt.Sprintf("PANIC: 文件读取失败，%s", err.Error()))
		}
		TypeMointorChan <- TypeHandLeLine
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
			TypeMointorChan <- TypeErrNum
			log.Println("时间格式不对", err.Error(), ret[4])
			continue
		}
		message.TimeLocal = time

		byteSent, _ := strconv.Atoi(ret[8])
		message.BytesSent = byteSent

		reqLine := strings.Split(ret[6], " ")
		if len(reqLine) != 3 {
			TypeMointorChan <- TypeErrNum
			log.Println("请求行解析失败：", ret[6])
			continue
		}
		message.Method = reqLine[0]
		url, err := url.Parse(reqLine[1])
		if err != nil {
			TypeMointorChan <- TypeErrNum
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
		TypeMointorChan <- TypeErrNum
		log.Fatalf("读取写入信息失败,%s", err.Error())
	}
	defer c.Close()

	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  lwslice[3],
		Precision: lwslice[4],
	})
	if err != nil {
		TypeMointorChan <- TypeErrNum
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
			TypeMointorChan <- TypeErrNum
			log.Fatal(err)
		}
		bp.AddPoint(pt)

		// Write the batch
		if err := c.Write(bp); err != nil {
			TypeMointorChan <- TypeErrNum
			log.Fatal(err)
		}
		// Close client resources
		if err := c.Close(); err != nil {
			TypeMointorChan <- TypeErrNum
			log.Fatal(err)
		}
		log.Println("写入成功")
	}
}

// 程序的监控模块
// 思路：首先创建传送数据的结构体。包含需要监控的属性。这样就可以通过创建http服务来访问得到。（感觉可以直接集成到的写入模块的log里面显示，但是业务中应该不是这样，应该还是另外起一个模块）
// 首先创建需要接收json数据的结构体
// 系统状态监控，需要监控的信息
type Systemmonitor struct {
	HandleLine   int     `json:"handleLine"`   // 总处理日志行数
	Tps          float64 `json:"tps"`          // 系统吞吐量。日志数/处理时间。定时获得handleline
	ReadChanLen  int     `json:"readChanLen"`  // read channel长度
	WriteChanLen int     `json:"writeChanLen"` // write channel长度
	RunTime      string  `json:"runTime"`      // 运行总时间
	ErrNum       int     `json:"errNum"`       // 错误数
}

// 作为监控模块的封装
type Monitor struct {
	startTime time.Time
	data      Systemmonitor
	handleSli []int // 这里原来考虑的是直接定义长度为2的slice但是判断是否是空比较麻烦
}

const (
	TypeHandLeLine = 0
	TypeErrNum     = 1
)

// 创建channel之后考虑在哪里添加，这里有读取行数在读取方法中添加
var TypeMointorChan = make(chan int, 200)

// 方法实现，通过http方式暴露信息
func (monitor *Monitor) start(lp *LogProcess) {
	// 使用goroutine消费监控的日志数和错误数数据
	go func() {
		for n := range TypeMointorChan {
			switch n {
			case TypeHandLeLine:
				monitor.data.HandleLine++
			case TypeErrNum:
				monitor.data.ErrNum++
			}
		}
	}()
	// 接下来是吞吐量，思路是使用定时任务，定时获得handleline然后除以单位时间就是吞吐量
	ticker := time.NewTicker(5 * time.Second)
	// 还是使用协程来处理，不是很懂为什么需要协程处理，直接for循环不就行么。一直for循环
	// 好像知道了，使用goroutine就不是阻塞的了，这样就能在执行后面步骤的时候同步执行这个功能。
	go func() {
		for {
			<-ticker.C
			monitor.handleSli = append(monitor.handleSli, monitor.data.HandleLine)
			// 时刻保持slice的长度只能为2
			if len(monitor.handleSli) > 2 {
				monitor.handleSli = monitor.handleSli[1:]
			}
		}
	}()

	// 使用http暴露系统监控信息
	// 首先考虑比较容易实现的RunTime, ReadChanLen和WriteChanLen，需要传入参数
	http.HandleFunc("/monitor", func(writer http.ResponseWriter, req *http.Request) {
		// 将得到的信息存储到定义的json结构体里面
		// 之所以将这些属性放到hadnlefunc里面来赋值，是因为这些要求都是实时的。放在外面就不准确了，然后外面的errnum这种因为需要随时计算，因此放到外面，放到里面不会一直执行
		monitor.data.RunTime = time.Now().Sub(monitor.startTime).String()
		monitor.data.ReadChanLen = len(lp.rCh)
		monitor.data.WriteChanLen = len(lp.wCh)
		if len(monitor.handleSli) >= 2 {
			monitor.data.Tps = float64(monitor.handleSli[1] - monitor.handleSli[0]) / 5
		}
		// 接下来考虑handlerline和errnum，有两种方式，一种是全局变量，但是这种在goroutine中可能会冲突，因为有多个goroutine不能保证线程安全
		// 还有就是使用channel来做goroutine间的通信
		monitor.data.ErrNum = <- TypeMointorChan
		// 存完信息了直接将结构体渲染,marshalindent和marshal功能类似，只是使用了分隔符，更利于人看
		ret, _ := json.MarshalIndent(monitor.data, "", "\t")
		// 将内容输出到writer里面
		io.WriteString(writer, string(ret))
	})
	// 这里这样是阻塞的
	http.ListenAndServe(":9193", nil)
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

		// 优化思路，这里应该使用buffer的channel。否则只能存一个取一个.
		rCh: make(chan []byte, 200),
		wCh: make(chan *Message, 200),
		//path:         "/var/log/messages/",
		//influxdbinfo: "passworld&root",
	}
	// 优化思路，这里因为read的速度肯定最慢，然后是read，然后是write。因此这里多创建几个goroutine
	go loger.r.read(loger.rCh)
	for i := 0; i < 2; i++ {
		go loger.ParseFromRead()
	}
	for i := 0; i < 400; i++ {
		go loger.w.write(loger.wCh)
	}
	// 实例化monitor
	monitor := Monitor{
		startTime: time.Now(),
		data:      Systemmonitor{},
	}
	monitor.start(loger)

	// 有了start方法这里就不用了，因为start中的http.litenandserver是阻塞的
	//time.Sleep(60*time.Second)
}
