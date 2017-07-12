package main

// Given a centralised config, get expectations of packets being received. If they're not received, or they're late (latency)
// then error. Don't send errors too frequently.
// If we're responsible for sending packets then do that, and if they fail then report those errors too (ICMP etc). Again,
// don't report errors too quickly.
// If there was an error and it clears, send that too.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/beevik/ntp"
	"github.com/google/uuid"
	"github.com/op/go-logging"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	PEERPAIRVERSION = 1
)

var log = logging.MustGetLogger("peerpair")

type Alarmer interface {
	Start(hostname string) error
	ConfigError(hostname, configserver, failuretype string) error
	UnknownPacket(hostname, fromaddress, failuretype string) error
	ConnectionDown(hostname, fromaddress, toaddress, description, failuretype string) error
	ConnectionUp(hostname, fromaddress, toaddress, description string) error
}

type Commandline struct {
	ConfigServer *string
	LocalName    *string
	SNMPServer   *string // used for problesms prior to getting real servername from configserver
}

type GlobalConfig struct {
	Version                       int // monotonically increasing
	Enabled                       bool
	FailureLatencyEgregiousMicros int64 // Over this limit it's just impossible, which is a different kind of error
}

type TestMap map[uuid.UUID]*TestExecution
type TestExecution struct {
	T            *Test
	NextTest     time.Time // next send, or expiry of expectation
	LastPacket   time.Time
	LastError    time.Time
	OutOfService bool // be explicit about being broken, LastError should track time since last check
}

type HostConfig struct {
	Lock                sync.RWMutex
	Version             int // monotonically increasing
	Enabled             bool
	SNMPServer          string
	NTPServer           string
	MaxReportsPerSecond int
	CachePeriodSeconds  int
	SSHPubKey           string  // for access to this host
	Tests               TestMap `json:"-"` // not supplied, generated from other etcd keys
}

type Test struct {
	Id                   uuid.UUID
	Version              int // monotonically increasing
	Description          string
	Enabled              bool
	FromHostId           string
	From                 string
	ToHostId             string
	To                   string
	Protocol             string
	FreqSeconds          int
	FreqRetrySeconds     int
	FailureConnReportMin int // if this many packets fail then it's an error
	FailureLatencyMicros int // upper limit on latency, over this it's an error
}

type PeerTestPacket struct {
	Version       int
	ConfigVersion int // (monotonic inc)
	NtpTimestamp  time.Time
	HostId        string
	TestId        uuid.UUID
}

func main() {
	//Set error/logs
	logLevel := "DEBUG"

	beStdErr := logging.NewLogBackend(os.Stderr, "", 0)
	beSyslog, _ := logging.NewSyslogBackend("peerpair")
	besysl := logging.AddModuleLevel(beSyslog)
	bestdErrl := logging.AddModuleLevel(beStdErr)
	level, err := logging.LogLevel(logLevel)
	if err != nil {
		log.Fatal("Log level", logLevel, "is not valid")
	}
	besysl.SetLevel(level, "")
	bestdErrl.SetLevel(level, "")
	logging.SetBackend(besysl, bestdErrl)

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal("Could not get hostname", err)
	}
	pos := strings.Index(hostname, ".")
	if pos > 0 {
		hostname = hostname[:pos]
	}

	var cl Commandline
	cl.ConfigServer = flag.String("configserver", "", "DNS name/IP address of server to get config from. Port optional")
	cl.SNMPServer = flag.String("snmpserver", "", "SNMP server to use prior to config being read/in case config cannot be read")
	cl.LocalName = flag.String("localname", hostname, "Name to use to look up tests. Defaults to short hostname")
	flag.Parse()

	if *cl.ConfigServer == "" {
		log.Fatal("-configserver is a required parameter - please specify the etcd server to use by IP or DNS name")
	}

	log.Info("Reading config from:", *cl.ConfigServer, "for hostname:", *cl.LocalName)
	log.Info("initial SNMP Server: ", *cl.SNMPServer)

	var isnmp *Snmper
	if *cl.SNMPServer != "" {

		server, sport, err := net.SplitHostPort(*cl.SNMPServer)
		var port int
		if err != nil {
			port = 162
		} else {
			port, _ = strconv.Atoi(sport)
		}

		isnmp, err = snmpLogInit(server, uint16(port))
		if err != nil {
			log.Fatal("SNMP connection error to", *cl.SNMPServer)
		}
		defer isnmp.Conn.Close()
		isnmp.Start(*cl.LocalName)
	}

	// read config from server
	var gc GlobalConfig
	var hc HostConfig
	err = readConfig(cl, &gc, &hc)
	if err != nil {
		if isnmp != nil {
			isnmp.ConfigError(*cl.LocalName, *cl.ConfigServer, err.Error())
		}
		log.Fatal("Config read error:", err)
	}
	log.Info(gc)
	log.Info(hc)
	lastCache := time.Now()

	port := 0
	server, sport, err := net.SplitHostPort(hc.SNMPServer)
	if err != nil {
		port = 162
	} else {
		port, _ = strconv.Atoi(sport)
	}
	var snmp *Snmper
	snmp, err = snmpLogInit(server, uint16(port))
	defer snmp.Conn.Close()
	if err != nil {
		// TODO SNMP error trap
		log.Fatal("SNMP connection error to", hc.SNMPServer)
	}
	if isnmp != nil {
		isnmp.Conn.Close()
	}

	go goAlarmOnOverdue(cl, &gc, &hc, snmp)
	main := make(chan PeerTestPacket)
	listeners := []chan bool{}
	if gc.Enabled {
		listeners = startListeners(cl, hc, main)
	}

	// loop
	for {
		if lastCache.Add(time.Duration(hc.CachePeriodSeconds) * time.Second).Before(time.Now()) {
			log.Info("Refreshing config, stale after ", hc.CachePeriodSeconds, "seconds")
			var newhc HostConfig
			err = readConfig(cl, &gc, &newhc)
			if err != nil {
				snmp.ConfigError(*cl.LocalName, *cl.ConfigServer, err.Error())
			} else {
				// copy the old test results over
				hc.Lock.Lock()
				testmap := hc.Tests
				lock := hc.Lock
				hc = newhc
				hc.Lock = lock
				for uuid, t := range hc.Tests {
					if val, ok := testmap[uuid]; ok {
						t.OutOfService = val.OutOfService
						t.NextTest = val.NextTest
						t.LastPacket = val.LastPacket
						t.LastError = val.LastError
					}
				}
				hc.Lock.Unlock()
				for _, l := range listeners {
					l <- true
				}
				// wait for all listeners to acknowledge their deaths
				for _, l := range listeners {
					<-l // will block if not close yet
				}
				if gc.Enabled {
					listeners = startListeners(cl, hc, main)
				} else {
					listeners = []chan bool{}
				}
			}
			lastCache = time.Now()
			snmp.ConfigRead(*cl.LocalName, *cl.ConfigServer) // clear existing error, if any
		}
		uuid, err := findNextSend(hc.Tests, *cl.LocalName)
		if err == nil {
			if gc.Enabled && hc.Tests[uuid].T.Enabled && hc.Tests[uuid].NextTest.Before(time.Now()) {
				err = sendTestPacket(hc, *hc.Tests[uuid].T)
				hc.Lock.Lock()
				if err != nil {
					log.Info("Test", hc.Tests[uuid].T.Description, "failed with error", err)
					hc.Tests[uuid].LastError = time.Now()
					hc.Tests[uuid].NextTest = time.Now().Add(time.Duration(hc.Tests[uuid].T.FreqRetrySeconds) * time.Second)
				} else {
					hc.Tests[uuid].LastPacket = time.Now()
					hc.Tests[uuid].NextTest = time.Now().Add(time.Duration(hc.Tests[uuid].T.FreqSeconds) * time.Second)
				}
				hc.Lock.Unlock()
			}
		}

		for {
			found := true
			select {
			case p := <-main:
				// no need to lock as only this loop is writing, and any conflict results in an acceptable outcome
				t, ok := hc.Tests[p.TestId]
				if !ok || p.HostId == *cl.LocalName {
					log.Error("Invalid test packet received, Id:", p.TestId, "for host:", p.HostId)
					continue
				}
				ts := time.Now()
				if ts.Sub(p.NtpTimestamp) > time.Duration(t.T.FailureLatencyMicros)*time.Microsecond &&
					ts.Sub(p.NtpTimestamp) < time.Duration(gc.FailureLatencyEgregiousMicros)*time.Microsecond {
					snmp.ConnectionDown(*cl.LocalName, t.T.From, t.T.To, t.T.Description, fmt.Sprintf("High Latency %f Seconds", ts.Sub(p.NtpTimestamp).Seconds()))
					t.OutOfService = true
					t.LastError = time.Now()
				} else {
					if t.OutOfService {
						snmp.ConnectionUp(*cl.LocalName, t.T.From, t.T.To, t.T.Description)
						t.OutOfService = false
					}
				}
				t.LastPacket = ts
				t.NextTest = ts.Add(time.Duration(t.T.FreqSeconds) * time.Second) // reset the due time
			default:
				found = false
				break
			}
			if !found {
				break
			}
		}

		if !gc.Enabled { // sleep if there is nothing to do
			time.Sleep(1 * time.Second)
		}

	}
}

func readConfig(cl Commandline, gc *GlobalConfig, hc *HostConfig) error {

	type EtcdNode struct {
		Key   string
		Value string
		Dir   bool
		Nodes []EtcdNode
	}

	type EtcdGet struct {
		Action string
		Node   EtcdNode
	}

	// GlobalConfig
	resp, err := http.Get(fmt.Sprintf("http://%s/v2/keys/peerpair/config", *cl.ConfigServer))
	if err != nil {
		log.Info("Error retrieving global config", err)
		return err
	}
	if resp.StatusCode != 200 {
		return errors.New("etcd not accessible")
	}
	var gcget EtcdGet
	var newgc GlobalConfig
	if err := json.NewDecoder(resp.Body).Decode(&gcget); err != nil {
		log.Info("Error parsing global config", err)
		return err
	}
	if err := json.NewDecoder(strings.NewReader(gcget.Node.Value)).Decode(&newgc); err != nil {
		log.Errorf("GlobalConfig failed to parse. Error %s", err)
		return err
	}
	// overwrite global config immediately if it worked - global enable flag should be respected even if the rest fail
	*gc = newgc

	// HostConfig
	resp, err = http.Get(fmt.Sprintf("http://%s/v2/keys/peerpair/hosts/%s", *cl.ConfigServer, *cl.LocalName))
	if err != nil {
		log.Info("Error retrieving config for host", *cl.LocalName)
		return err
	}
	var hcget EtcdGet
	var newhc HostConfig
	newhc.Tests = make(TestMap)
	if err := json.NewDecoder(resp.Body).Decode(&hcget); err != nil {
		log.Info("Error parsing config for host", *cl.LocalName)
		return err
	}
	if err := json.NewDecoder(strings.NewReader(hcget.Node.Value)).Decode(&newhc); err != nil {
		log.Errorf("HostConfig %s failed to parse. Error %s", *cl.LocalName, err)
		return err
	}

	// Tests
	resp, err = http.Get(fmt.Sprintf("http://%s/v2/keys/peerpair/tests/%s/", *cl.ConfigServer, *cl.LocalName))
	if err != nil {
		log.Info("Error retrieving tests for host", *cl.LocalName)
		return err
	}
	var keys EtcdGet
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		log.Info("Error parsing tests for host", *cl.LocalName)
		return err
	}

	for _, n := range keys.Node.Nodes {
		var t Test
		if err := json.NewDecoder(strings.NewReader(n.Value)).Decode(&t); err != nil {
			log.Error("Test %s/%s failed to parse, skipping", *cl.LocalName, n.Key)
			continue
		}
		log.Info(t.Id, t.Description, t.Enabled)
		if t.Enabled { // don't bother loading disabled tests, it makes it harder to iterate over the list
			next := time.Now()
			if t.ToHostId == *cl.LocalName {
				next = next.Add(time.Duration(t.FreqSeconds) * time.Second)
			}
			newhc.Tests[t.Id] = &TestExecution{T: &t, NextTest: next}
		}
	}

	*hc = newhc
	log.Info(*gc, hc)
	return nil
}

func sendTestPacket(hc HostConfig, test Test) error {
	log.Info("Sending test packet for :", test.Description)
	if test.Protocol != "udp" {
		return errors.New("non-UDP not supported")
	}

	from, err := net.ResolveUDPAddr(test.Protocol, test.From)
	if err != nil {
		log.Info(err)
		return err
	}
	to, err := net.ResolveUDPAddr(test.Protocol, test.To)
	if err != nil {
		log.Info(err)
		return err
	}
	log.Info("From:", from)
	log.Info("To:", to)
	conn, err := net.DialUDP(test.Protocol, from, to)
	if err != nil {
		log.Info(err)
		return err
	}
	defer conn.Close()

	ts, err := ntp.Time("0.pool.ntp.org")
	if err != nil {
		log.Error(err)
		ts = time.Now() //fallback to local timesource
	}

	p := PeerTestPacket{
		Version:       PEERPAIRVERSION,
		ConfigVersion: hc.Version,
		NtpTimestamp:  ts,
		HostId:        test.FromHostId,
		TestId:        test.Id,
	}

	jsudp := json.NewEncoder(conn)
	err = jsudp.Encode(p)
	if err != nil {
		return err
	}
	return nil
}

func findNextSend(tests TestMap, hostname string) (uuid.UUID, error) {
	var earliest time.Time
	var uuid uuid.UUID

	if len(tests) == 0 {
		log.Info("No tests")
		return uuid, errors.New("No tests")
	}
	first := true
	for _, t := range tests {
		if first && t.T.FromHostId == hostname {
			uuid = t.T.Id
			first = false
		}
		if t.T.FromHostId == hostname && t.NextTest.Before(earliest) {
			earliest = t.NextTest
			uuid = t.T.Id
		}
	}
	if first {
		log.Info("No tests")
		return uuid, errors.New("No tests")
	}
	return uuid, nil
}

func startListeners(cl Commandline, hc HostConfig, main chan PeerTestPacket) []chan bool {
	var listeners []chan bool

	log.Info("Loading tests to listen for")

	type ProtoAddr struct {
		Proto string
		Addr  string
	}
	listenAddresses := make(map[ProtoAddr]bool)
	for _, t := range hc.Tests {
		if t.T.ToHostId == *cl.LocalName {
			listenAddresses[ProtoAddr{Proto: t.T.Protocol, Addr: t.T.To}] = true
		}
	}

	for p, _ := range listenAddresses {
		cb := make(chan bool)
		go listenForPackets(p.Proto, p.Addr, cb, main)
		listeners = append(listeners, cb)
	}

	return listeners

}

func listenForPackets(proto, addr string, die chan bool, main chan PeerTestPacket) {
	log.Info("Listening to", proto, addr)
	to, err := net.ResolveUDPAddr(proto, addr)
	if err != nil {
		log.Error(err)
		return
	}
	u, err := net.ListenUDP("udp", to)
	if err != nil {
		log.Error(err)
		return
	}
	u.SetReadBuffer(576 * 10)

	buf := make([]byte, 1024)
	var p PeerTestPacket
	for {
		select {
		case <-die:
			log.Info("Listener on", proto, addr, "dying on command")
			u.Close()
			die <- true // release the semaphore
			return
		default:
		}
		u.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, raddr, err := u.ReadFromUDP(buf)
		if err == nil {
			if err := json.Unmarshal(buf, &p); err != nil {
				log.Error("Undecodable packet from", raddr.String(), "to", u.LocalAddr().String(), "error decoding", err)
			}
			main <- p
		}

	}

}

func goAlarmOnOverdue(cl Commandline, gc *GlobalConfig, hc *HostConfig, a Alarmer) {

	for {
		if gc.Enabled {
			hc.Lock.Lock() // avoid the tests changing under our feet
			for _, t := range hc.Tests {
				if t.T.ToHostId == *cl.LocalName && t.T.Enabled {
					//log.Info("Checking for packets for test", t.T.Id, t.T.Description, "expires in", t.NextTest.Sub(time.Now()).Seconds())
					if !t.OutOfService && t.NextTest.Before(time.Now()) {
						a.ConnectionDown(*cl.LocalName, t.T.From, t.T.To, t.T.Description, fmt.Sprintf("Timeout, no packet received in %d seconds", t.T.FreqSeconds))
						t.OutOfService = true
						t.LastError = time.Now()
					}
				}
			}
			hc.Lock.Unlock()
		}
		time.Sleep(1 * time.Second) // per-second reporting will be enough
	}
}
