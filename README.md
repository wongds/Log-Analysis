# Log-Analysis
> 日志分析处理监控demo
#### 流程
> - goroutine 读取模块从log.txt文件中读取内容，将内容写入到读取channel
> - goroutine 解析模块获取readchannel中的内容，使用正则表达式筛选读取的内容，并解析结果到writechannel
> - goroutine 写入模块获得writechannel中的内容，获得influxdb连接信息，创建client将writechannel中的内容解析存入到influxdb中
> - 监控模块获得程序运行时的处理日志数、运行时间、readchannel长度、writechannel长度、错误数、吞吐量等等。并使用httpserver暴露到9193端口

#### 使用说明
> 首先启动influxdb和grafana(注意项目中数据库地址信息的更改)
> 接下来运行mock_data.go模拟nginx日志写入到log.txt中去
> 然后执行go run log_process.go -path="日志文件地址" --influxdb="数据库信息字符串(http://192.168.75.144:8086&admin&admin&MYDB&s)"
> 看到写入成功log。之后访问grafana。更改datasource，查看监控模块监控信息
> 执行curl 127.0.0.1:9193/monitor查看项目运行信息