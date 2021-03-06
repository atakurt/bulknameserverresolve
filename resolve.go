package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var verbose = false
var dnsFile string
var workerCount int
var retryCount int
var nameserver string
var nameserverPort int
var outputType string
var timeout time.Duration

type resolveResult struct {
	domain      string
	nameservers []string
	err         string
	status      string
}

func main() {
	parseArgs()

	dnsQueue := make(chan string, 1000000)
	dnsResults := make(chan resolveResult, 1000000)

	var wg sync.WaitGroup

	var domainCount = addDomainsToResolveQueue(dnsQueue, dnsFile)
	logIt("Number of domains to resolve", domainCount)

	wg.Add(workerCount)

	for i := 1; i <= workerCount; i++ {
		go resolve(&wg, i, dnsQueue, dnsResults)
	}

	for i := 1; i <= domainCount; i++ {
		printDNSResult(<-dnsResults)
	}

	wg.Wait()
}

func printDNSResult(result resolveResult) {

	if outputType == "struct" {
		fmt.Println(result)
	} else {
		var nameservers string
		switch len(result.nameservers) {
		case 0:
			nameservers = "\t\t"
		case 1:
			nameservers = result.nameservers[0] + "\t"
		default:
			nameservers = strings.Join(result.nameservers[0:2], "\t")
		}
		fmt.Println(result.domain, "\t", nameservers, result.status)
	}
}

func logIt(v ...interface{}) {
	if verbose {
		log.Println(v)
	}
}

func resolve(wg *sync.WaitGroup, id int, dnsQueue <-chan string, dnsResults chan<- resolveResult) {
	defer wg.Done()
	for domain := range dnsQueue {
		nameServer := nameserver + ":" + strconv.Itoa((nameserverPort))
		logIt("worker", id, "resolving", domain, "from", nameServer)

		var result []string
		var err error
		var t time.Duration
		var isSuccess = false
		var status string

		result, t, status, err = resolveNS(domain, nameServer)
		isSuccess = status == "SUCCESS"

		if !isSuccess {
			for i := retryCount; i > 0; i-- {
				result, t, status, err = resolveNS(domain, nameServer)
				isSuccess = status == "SUCCESS"
				if isSuccess {
					break
				}
				logIt("worker", id, "retrying", domain, "#", (retryCount-i)+1, " time")
			}
		}

		logIt("worker", id, "resolved", domain, "as", result, "in", t)

		var errorMessage string

		if nil != err {
			errorMessage = err.Error()
		}

		dnsResults <- resolveResult{domain: domain, nameservers: result, err: errorMessage, status: status}
	}
}

func isTimeout(err error) bool {
	return nil != err && strings.HasSuffix(err.Error(), "i/o timeout")
}

func resolveNS(domain string, nameServer string) (r []string, t time.Duration, status string, err error) {
	c := dns.Client{}
	c.Timeout = timeout
	m := dns.Msg{}
	m.SetQuestion(domain+".", dns.TypeNS)
	response, t, err := c.Exchange(&m, nameServer)

	status = "SUCCESS"
	if err != nil {
		if isTimeout(err) {
			status = "TIMEOUT"
		} else {
			status = "ERROR"
		}
	}

	if nil == response || (nil == err && len(response.Answer) == 0) {
		status = "NO_RESPONSE"
	}

	result := []string{}
	if nil == err && len(response.Answer) != 0 {
		for _, ans := range response.Answer {
			Arecord, ok := ans.(*dns.NS)
			if ok {
				result = append(result, Arecord.Ns)
			}
		}
	}
	return result, t, status, err
}

func addDomainsToResolveQueue(dnsQueue chan<- string, fileName string) int {
	file, err := os.Open(fileName)
	defer file.Close()

	if err != nil {
		log.Fatal(err)
	}

	var domainCount = 0
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	for {
		success := scanner.Scan()
		if success == false {
			err = scanner.Err()
			if err == nil {
				logIt("Input file scan complete")
			} else {
				log.Fatal(err)
			}
			break
		} else {
			dnsQueue <- scanner.Text()
			domainCount++
		}
	}

	close(dnsQueue)
	return domainCount
}

func parseArgs() {
	verbosePtr := flag.Bool("v", false, "verbose")
	workerPtr := flag.Int("w", runtime.NumCPU(), "worker count")
	retryPtr := flag.Int("r", 5, "retry count")
	timeoutPtr := flag.Duration("t", 10*time.Second, "timeout duration")
	inputPtr := flag.String("i", "", "domain list")
	nameserverPtr := flag.String("ns", "8.8.8.8", "nameserver")
	nameserverPortPtr := flag.Int("nsp", 53, "nameserver port")
	outPtr := flag.String("o", "tsv", "output type: tsv,struct")

	flag.Parse()

	verbose = *verbosePtr
	workerCount = *workerPtr
	retryCount = *retryPtr
	nameserver = *nameserverPtr
	outputType = *outPtr
	timeout = *timeoutPtr
	nameserverPort = *nameserverPortPtr

	if "" == *inputPtr {
		flag.Usage()
		os.Exit(1)
	} else {
		dnsFile = *inputPtr
	}
}
