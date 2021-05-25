package main

import (
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Server ...
type Server struct {
	ServerName               string
	ServerConfig             ServerConfig
	online                   bool
	keepAlive                bool
	InChan, OutChan, ErrChan chan string
	cmdChan                  chan string
	stdin                    io.WriteCloser
	stdout, stderr           io.ReadCloser
	cmd                      *exec.Cmd
}

// NewServer ...
func NewServer(ServerName string, ServerConfig ServerConfig) *Server {
	return &Server{
		ServerName:   ServerName,
		ServerConfig: ServerConfig,
		InChan:       make(chan string, 8),
		OutChan:      make(chan string, 8),
		cmdChan:      make(chan string),
	}
}

func (server *Server) Init() {
	args := append(strings.Split(server.ServerConfig.ExecOptions, " "), "-jar",
		server.ServerConfig.ExecPath, "--nogui")
	cmd := exec.Command("java", args...)
	cmd.Dir = filepath.Dir(server.ServerConfig.ExecPath)

	server.cmd = cmd
	server.stdin, _ = cmd.StdinPipe()
	server.stdout, _ = cmd.StdoutPipe()
	server.stderr, _ = cmd.StderrPipe()
	// fmt.Println(server)
}

func (server *Server) Write(str string) {
	server.stdin.Write([]byte(str + "\n"))
}

func (server *Server) Tick(wg *sync.WaitGroup) {
	if err := server.cmd.Wait(); err != nil {
		log.Panicf("server<%s>: Error when running:\n%s", server.ServerName, err.Error())
	}
	server.online = false
	if !server.keepAlive {
		wg.Done()
	}
}

// Run ...
func (server *Server) Run(wg *sync.WaitGroup) {
	server.Init()
	// fmt.Print(server)
	defer func() {
		if err := recover(); err != nil {
			server.online = false
		}
	}()

	if err := server.cmd.Start(); err != nil {
		log.Panicf("server<%s>: Error when starting:\n%s", server.ServerName, err.Error())
	}
	if !server.keepAlive {
		wg.Add(1)
	}
	server.online = true

	go forwardStd(server.stdout, server.OutChan)
	go forwardStd(server.stderr, server.ErrChan)

	go server.processIn()
	go server.processOut()
	go server.processErr()

	go server.handleCommand()
	go server.Tick(wg)
}

func forwardStd(f io.ReadCloser, c chan string) {
	defer func() {
		recover()
	}()
	cache := ""
	buf := make([]byte, 1024)
	for {
		num, err := f.Read(buf)
		if err != nil && err != io.EOF { //非EOF错误
			log.Panicln(err)
		}
		if num > 0 {
			str := cache + string(buf[:num])
			lines := strings.SplitAfter(str, "\n") // 按行分割开
			for i := 0; i < len(lines)-1; i++ {
				c <- lines[i]
			}
			cache = lines[len(lines)-1] //最后一行下次循环处理
		}
	}
}

func (server *Server) handleCommand() {
	for {
		line := <-server.cmdChan
		words := strings.Split(line, " ")
		args := []string{""}
		if len(words) > 1 {
			args = words[1:]
		}
		var cmdFun, exist = Cmds[words[0]]
		if exist {
			cmdFun.(func(server *Server, args []string) error)(server, args)
		}
	}
}

func (server *Server) processIn() {
	for {
		line := <-server.InChan
		// fmt.Println(line)
		if line[:1] == MCSHConfig.CommandPrefix {
			server.cmdChan <- line[1:]
		} else {
			server.stdin.Write([]byte(line + "\n"))
		}
	}
}
func (server *Server) processOut() {
	for {
		line := <-server.OutChan
		// 去掉换行符
		if i := strings.LastIndex(string(line), "\r"); i > 0 {
			line = line[:i]
		} else {
			line = line[:len(line)-1]
		}
		if res := playerOutputReg.FindStringSubmatch(line); len(res) > 1 { // Player
			player := res[1]
			text := res[2]
			log.Println(player + ": " + text)
			if text[:1] == MCSHConfig.CommandPrefix {
				server.cmdChan <- text[1:]
			}
		}
		str := outputFormatReg.ReplaceAllString(line, "["+server.ServerName+"/$2]") // 格式化读入的字符串
		log.Print(str)
	}
}
func (server *Server) processErr() {
	for {
		line := <-server.ErrChan
		log.Print(line)
	}
}