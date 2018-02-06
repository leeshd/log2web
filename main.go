package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var hostname string
var port string
var wsurl string

func init() {
	flag.StringVar(&hostname, "h", "localhost", "please input hostname use -h=hostname, default localhost")
	flag.StringVar(&port, "p", "6699", "please input port use -p=port, default 6699")
	flag.StringVar(&wsurl, "w", "6699", "please input port use -w=ws://localhost:6699/logws, default 6699")
}

func main() {
	flag.Parse()
	container := gin.Default()
	container.Use(gin.Logger())
	container.Use(gin.Recovery())
	wslsnr := NewWSService(container)
	go func(lsnr *WSListener) {
		stdin := getStd(os.Stdin)
		stdout := getStd(os.Stdout)
		stderr := getStd(os.Stderr)

		bfs := make([]*bufio.Reader, 0)
		if stdin != nil {
			bfs = append(bfs, bufio.NewReader(stdin))
		}
		if stdout != nil {
			bfs = append(bfs, bufio.NewReader(stdout))
		}
		if stderr != nil {
			bfs = append(bfs, bufio.NewReader(stderr))
		}
		if len(bfs) > 0 {
			for {
				for _, bfr := range bfs {
					linebyt, _, err := bfr.ReadLine()
					if err != nil && err == io.EOF {
						fmt.Println("exit")
						os.Exit(-1)
						return
					}
					line := string(linebyt)
					line = line + "\n"
					lsnr.pipwriter.Write([]byte(line))
				}

			}
		}
	}(wslsnr)

	fmt.Println("start listen ", port)
	fmt.Println("access log by http://" + hostname + ":" + port + "/log")
	fmt.Println("how to useage: {your_process} 2>&1 | log2web -h={your_hostname} -p={your_port} -w={your_external_ws_uri}")
	fmt.Println("      example: ./mytest 2>&1 | log2web -h=localhost -p=6699 -w=ws://localhost:6699/logws")
	container.Run(":" + port)
}

func getStd(fd *os.File) *os.File {
	fi, err := fd.Stat()
	if err != nil {
		fmt.Println("is not std file!")
		return nil
	}
	if fi.Mode()&os.ModeNamedPipe == 0 {
		return nil
	} else {
		fmt.Println("hi pipe!")
		return fd
	}
}

type client struct {
	id       string
	conn     *websocket.Conn
	lsnr     *WSListener
	closesig chan bool
	datach   chan []byte
}

func newclient(c *websocket.Conn, wslsnr *WSListener) *client {
	uuid8 := GenUUIDLen8()
	datach := make(chan []byte, 10000)

	return &client{id: uuid8, conn: c, lsnr: wslsnr, datach: datach}
}

func (this *client) writeData(data []byte) {
	this.datach <- data
}

func (this *client) write(data []byte) error {
	err := this.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		fmt.Println(err)

	}
	return err
}

func (this *client) read() ([]byte, error) {
	_, data, err := this.conn.ReadMessage()
	if err != nil {
		fmt.Println(err)
	}
	return data, err
}

func (this *client) listen() {
	fmt.Println("start listen")

	go func(cli *client) {
		for {

			select {
			case <-cli.closesig:
				close(cli.closesig)
				break
			default:
				fmt.Println("start recieve data")
				data, err := cli.read()
				fmt.Println("recieved data ", len(data))
				if err != nil {
					fmt.Println(err)

					cli.shutdown()
					break
				}
				if len(data) < 5 {
					fmt.Println("data is useless")
					continue
				}

			}
		}

		fmt.Println("end cli")

	}(this)

	go func(cli *client) {
		for {
			data := <-cli.datach
			err := cli.write(data)
			if err != nil {
				fmt.Println(err)
				os.Exit(2)
				cli.shutdown()
				break
			}
		}
	}(this)

}

// 关闭客户端
func (this *client) shutdown() {
	// 向本连接的读取线程发送关闭信号,
	this.closesig <- true
	//从监听器中移除该连接
	delete(this.lsnr.clientPool, this.id)
	// 关闭连接
	this.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Time{})
	this.conn.Close()
}

func (this *client) Close() {
	// 向本连接的读取线程发送关闭信号,
	fmt.Println(this.id, " closed")
	//从监听器中移除该连接
	delete(this.lsnr.clientPool, this.id)
	// 关闭连接
	this.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Time{})
	this.conn.Close()
}

type WSListener struct {
	clientPool map[string]*client
	userApps   map[string]string
	callCount  map[string]int
	pipreader  *io.PipeReader
	pipwriter  *io.PipeWriter
	wsupgrader *websocket.Upgrader
}

func NewWSListener() *WSListener {

	wsu := &websocket.Upgrader{
		ReadBufferSize:  40960,
		WriteBufferSize: 40960,
	}
	wsu.CheckOrigin = func(r *http.Request) bool {
		return true
	}
	pipR, pipW := io.Pipe()

	return &WSListener{
		clientPool: make(map[string]*client),

		userApps:   make(map[string]string),
		callCount:  make(map[string]int),
		pipreader:  pipR,
		pipwriter:  pipW,
		wsupgrader: wsu,
	}
}

func (this *WSListener) Accept(ctx *gin.Context) {
	fmt.Println("start accept")
	conn, err := this.wsupgrader.Upgrade(ctx.Writer, ctx.Request, nil)

	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("get connection and build client")
	cli := newclient(conn, this)
	this.clientPool[cli.id] = cli
	cli.listen()
}

func (this *WSListener) startRun() {

	// 将日志发送给客户端,群发
	go func(lsnr *WSListener) {

		bfr := bufio.NewReader(lsnr.pipreader)
		for {
			linebyt, _, err := bfr.ReadLine()

			if err != nil && err == io.EOF {
				fmt.Println("system exit")
				break
			}
			for cliName, cli := range this.clientPool {
				fmt.Println("write log to ", cliName, " :", string(linebyt))

				err := cli.write(linebyt)
				if err != nil {
					cli.Close()
				}

			}
		}
	}(this)

}

func NewWSService(container *gin.Engine) *WSListener {

	lsnr := NewWSListener()
	lsnr.startRun()
	container.Any("/logws", lsnr.Accept)
	container.GET("/log", LogWeb)
	container.Static("/log/res", GetStartPath()+"/res")
	return lsnr

}

func LogWeb(ctx *gin.Context) {
	bodybyts := []byte(`
    <!DOCTYPE html>
        <html>
        <head>
        <meta charset="utf-8">
        <title>tail log</title>
        <script src="https://cdn.bootcss.com/jquery/2.1.4/jquery.js"></script>
        <script type="text/javascript" src="./log/res/js/ansi_up.js"></script>
        
        </head>
        <body>
            <div id="log-container" style="font-size:14px; height: 768px; overflow-y: scroll; background: #333; color: #aaa; padding: 10px;">
                <div>
                </div>
            </div>
            <div id="terminal"></div>
        </body>
        <script>
            $(document).ready(function() {
                 var ansi_up = new AnsiUp;
                // 指定websocket路径
                var websocket = new WebSocket('` + wsurl + `');
                console.log("connect websocket")
                websocket.onmessage = function(event) {
                    var text = ansi_up.ansi_to_html(event.data);
                    console.log(text);
                 
                    // 接收服务端的实时日志并添加到HTML页面中
                    $("#log-container div").append(text+'<br/>');
                    // 滚动条滚动到最低部
                    $("#log-container").scrollTop($("#log-container div").height() - $("#log-container").height());
                };
                websocket.onopen = function() {
                    console.log("Socket 已打开");
                };
               
                websocket.onclose = function() {
                    console.log("Socket已关闭");
                };
                websocket.onerror = function(event) {
                    console.log("occur error")
                    console.log(event);
                };
            });
        </script>
        </body>
        </html>
    `)
	ctx.Data(200, "text/html", bodybyts)
}

var HerricUUIDSeed *CeUIdGen

func GenUUIDLen8() string {
	if HerricUUIDSeed == nil {
		HerricUUIDSeed = NewCeUIdGen()
	}
	return HerricUUIDSeed.GenUId()
}

func GetStartPath() string {
	file, _ := exec.LookPath(os.Args[0])
	path, _ := filepath.Abs(file)
	dir := filepath.Dir(path)
	return dir
}
