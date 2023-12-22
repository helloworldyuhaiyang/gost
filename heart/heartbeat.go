package heart

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	qg "github.com/lucas-clemente/quic-go"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/go-log/log"
)

type Args struct {
	HostName string
	// 上报的端口
	ReportPorts []int
	// ppp 拨号的间隔时间
	PppInterval int
	// 网卡名字
	NetDev string
	// 允许获取 ip 错误的次数
	MaxIpErrCount int
	// quic 服务器的地址
	ManagerAddr string
	// 跳过 tls 证书
	InsecureSkipVerify bool
	// ssl 证书
	SslCert string
	// 协议
	NextProtos []string
}

func NewHeartArgs() *Args {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	return &Args{
		HostName:           hostname,
		PppInterval:        30,
		ReportPorts:        []int{},
		NetDev:             "en0",
		MaxIpErrCount:      5,
		ManagerAddr:        "127.0.0.1:33258",
		InsecureSkipVerify: true,
		SslCert:            "./config/mangu-server.crt",
		NextProtos:         []string{"test"},
	}
}

// Heart 定时服务
type Heart struct {
	cfg *Args
	// 退出
	stopChan chan interface{}
	// 获取 ip 失败的次数
	getIpErrCount int

	// report 的 lock
	reportMutex sync.Mutex
	// quic 客户端
	sess qg.Session
	// 上次使用的 quic stream
	sendStream qg.SendStream
}

func NewHeart(cfg *Args) *Heart {
	return &Heart{
		cfg:      cfg,
		stopChan: make(chan interface{}),
	}
}

func (h *Heart) Name() string {
	return "heart"
}

func (h *Heart) Init() error {
	if h.cfg.PppInterval < 5 {
		return errors.New("pppInterval lt 5s")
	}

	return nil
}

func (h *Heart) Start() error {
	// 定时拨号
	go func() {
		timeInterval := time.Second * time.Duration(h.cfg.PppInterval)
		ppoeTimer := time.NewTimer(timeInterval)

		for {
			select {
			case <-h.stopChan:
				break
			case <-ppoeTimer.C:
				if err := h.restartPPPoe(); err != nil {
					log.Logf("restart pppoe error, err: %v", err)
				}
				h.reportHeart(int64(h.cfg.PppInterval))
				ppoeTimer.Reset(timeInterval)
			}
		}
	}()

	log.Logf("heart start ok\n")
	return nil
}

func (h *Heart) StopGracefully(wait time.Duration) error {
	h.stopChan <- struct{}{}
	if h.sess != nil {
		err := h.sess.CloseWithError(qg.ApplicationErrorCode(0), "closed")
		if err != nil {
			return err
		}
	}

	log.Logf("exit heart ok\n")
	return nil
}

func (h *Heart) restartPPPoe() (err error) {
	log.Logf("restart pppoe")

	stopCmd := exec.Command("/usr/sbin/pppoe-stop")
	res, err1 := h.runCmd(stopCmd)
	if err1 != nil {
		err = fmt.Errorf("pppoe-stop, res: %s, err: %v\n", res, err1)
	}

	startCmd := exec.Command("/usr/sbin/pppoe-start")
	res, err2 := h.runCmd(startCmd)
	if err2 != nil {
		err = fmt.Errorf("pppoe-start, res: %s, err: %v\n", res, err2)
	}

	return err
}

type Package struct {
	HostName string
	Ip       string
	Port     int
	DeadLine int64
}

func (h *Heart) reportHeart(deadLine int64) {
	h.reportMutex.Lock()
	defer h.reportMutex.Unlock()

	// 获取 ip
	ip, err := h.getIp()
	if err != nil {
		h.getIpErrCount++
		log.Logf("get ip error, errCount:%v, err:%v\n", h.getIpErrCount, err)
		if h.getIpErrCount >= h.cfg.MaxIpErrCount {
			log.Log("get ip always error, exit")
			os.Exit(-1)
		}
		return
	}
	h.getIpErrCount = 0

	// 上报 心跳
	for _, po := range h.cfg.ReportPorts {
		pack := &Package{
			Ip:       ip,
			HostName: h.cfg.HostName,
			Port:     po,
			DeadLine: deadLine,
		}

		h.sendPack(pack)
	}
}

func (h *Heart) getIp() (ip string, err error) {
	devIps, err := DevIps()
	if err != nil {
		err = fmt.Errorf("get ip error, err:%v\n", err)
	}

	// 或者指定网卡的 ip
	ip, ok := devIps[h.cfg.NetDev]
	if !ok {
		err = fmt.Errorf("get %s ip failed\n", h.cfg.NetDev)
	}
	return
}

func (h *Heart) runCmd(cmd *exec.Cmd) (res string, err error) {
	var cmdBuffer bytes.Buffer
	cmd.Stdout = &cmdBuffer
	cmd.Stderr = &cmdBuffer

	// 执行
	err = cmd.Run()
	return cmdBuffer.String(), err
}

func (h *Heart) connectManager() error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: h.cfg.InsecureSkipVerify,
		//Certificates: "./config/mangu-server.crt",
		NextProtos: h.cfg.NextProtos,
	}
	conn, err := qg.DialAddr(h.cfg.ManagerAddr, tlsConf, nil)
	if err != nil {
		return err
	}

	h.sess = conn

	timeCtx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()

	h.sendStream, err = h.sess.OpenUniStreamSync(timeCtx)
	return nil
}
func (h *Heart) sendPack(p *Package) {
	if h.sess == nil || h.sendStream == nil {
		err := h.connectManager()
		if err != nil {
			log.Logf("connectManager error, err:%v", err)
			return
		}
	}

	data, _ := json.Marshal(p)
	data = append(data, byte('\n'))

	sendNum := 0
	for true {
		_, err := h.sendStream.Write(data)
		if err == nil {
			break
		}
		// 重连
		if err = h.connectManager(); err != nil {
			log.Logf("heart failed, connectManager error, err:%v\n", err)
		} else {
			log.Logf("reconnect successful\n")
		}
		sendNum++
		if sendNum > 3 {
			log.Logf("send failed, sendNum:%d\n", sendNum)
			time.Sleep(time.Second * time.Duration(sendNum))
			return
		}
	}

	log.Logf("send succ, pack: %v", string(data[:len(data)-1]))
	return
}

// DevIps 获取本地 网卡和对应的 ip
func DevIps() (map[string]string, error) {
	ips := make(map[string]string)

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, i := range interfaces {
		byName, err := net.InterfaceByName(i.Name)
		if err != nil {
			return nil, err
		}
		addresses, err := byName.Addrs()
		for _, v := range addresses {
			if ip, ok := v.(*net.IPNet); ok {
				if ip.IP.To4() != nil {
					ips[byName.Name] = ip.IP.String()
				}
			}
		}
	}
	return ips, nil
}
