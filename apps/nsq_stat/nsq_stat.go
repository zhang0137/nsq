// This is a utility application that polls /stats for all the producers
// of the specified topic/channel and displays aggregate stats

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bitly/nsq/internal/lookupd"
	"github.com/bitly/nsq/internal/util"
)

var (
	showVersion      = flag.Bool("version", false, "print version")
	topic            = flag.String("topic", "", "NSQ topic")
	channel          = flag.String("channel", "", "NSQ channel")
	statusEvery      = flag.Duration("status-every", -1, "(deprecated) duration of time between polling/printing output")
	interval         = flag.Duration("interval", 2*time.Second, "duration of time between polling/printing output")
	countNum         = numValue{}
	nsqdHTTPAddrs    = util.StringArray{}
	lookupdHTTPAddrs = util.StringArray{}
)

type numValue struct {
	isSet bool
	value int
}

func (nv *numValue) String() string { return "N" }

func (nv *numValue) Set(s string) error {
	value, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return err
	}
	nv.value = int(value)
	nv.isSet = true
	return nil
}

func init() {
	flag.Var(&nsqdHTTPAddrs, "nsqd-http-address", "nsqd HTTP address (may be given multiple times)")
	flag.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	flag.Var(&countNum, "count", "number of reports")
}

func statLoop(interval time.Duration, topic string, channel string,
	nsqdTCPAddrs []string, lookupdHTTPAddrs []string) {
	var o *lookupd.ChannelStats
	for i := 0; !countNum.isSet || countNum.value >= i; i++ {
		var producers []string
		var err error

		log.SetOutput(ioutil.Discard)
		if len(lookupdHTTPAddrs) != 0 {
			producers, err = lookupd.GetLookupdTopicProducers(topic, lookupdHTTPAddrs)
		} else {
			producers, err = lookupd.GetNSQDTopicProducers(topic, nsqdHTTPAddrs)
		}
		log.SetOutput(os.Stdout)
		if err != nil {
			log.Fatalf("ERROR: failed to get topic producers - %s", err)
		}

		log.SetOutput(ioutil.Discard)
		_, allChannelStats, err := lookupd.GetNSQDStats(producers, topic)
		log.SetOutput(os.Stdout)
		if err != nil {
			log.Fatalf("ERROR: failed to get nsqd stats - %s", err)
		}

		c, ok := allChannelStats[channel]
		if !ok {
			log.Fatalf("ERROR: failed to find channel(%s) in stats metadata for topic(%s)", channel, topic)
		}

		if i%25 == 0 {
			fmt.Printf("%s+%s+%s\n",
				"------rate------",
				"----------------depth----------------",
				"--------------metadata---------------")
			fmt.Printf("%7s %7s | %7s %7s %7s %5s %5s | %7s %7s %12s %7s\n",
				"ingress", "egress",
				"total", "mem", "disk", "inflt",
				"def", "req", "t-o", "msgs", "clients")
		}

		if o == nil {
			o = c
			time.Sleep(interval)
			continue
		}

		// TODO: paused
		fmt.Printf("%7d %7d | %7d %7d %7d %5d %5d | %7d %7d %12d %7d\n",
			int64(float64(c.MessageCount-o.MessageCount)/interval.Seconds()),
			int64(float64(c.MessageCount-o.MessageCount-(c.Depth-o.Depth))/interval.Seconds()),
			c.Depth,
			c.MemoryDepth,
			c.BackendDepth,
			c.InFlightCount,
			c.DeferredCount,
			c.RequeueCount,
			c.TimeoutCount,
			c.MessageCount,
			c.ClientCount)

		o = c
		time.Sleep(interval)
	}
	os.Exit(0)
}

func checkAddrs(addrs []string) error {
	for _, a := range addrs {
		if strings.HasPrefix(a, "http") {
			return errors.New("address should not contain scheme")
		}
	}
	return nil
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("nsq_stat v%s\n", util.BinaryVersion)
		return
	}

	if *topic == "" || *channel == "" {
		log.Fatal("--topic and --channel are required")
	}

	intvl := *interval
	if *statusEvery != -1 {
		log.Printf("--status-every is deprecated, use --interval")
		intvl = *statusEvery
	}
	if int64(intvl) <= 0 {
		log.Fatal("--interval should be positive")
	}

	if countNum.isSet && countNum.value <= 0 {
		log.Fatal("--count should be positive")
	}

	if len(nsqdHTTPAddrs) == 0 && len(lookupdHTTPAddrs) == 0 {
		log.Fatal("--nsqd-http-address or --lookupd-http-address required")
	}
	if len(nsqdHTTPAddrs) > 0 && len(lookupdHTTPAddrs) > 0 {
		log.Fatal("use --nsqd-http-address or --lookupd-http-address not both")
	}

	if err := checkAddrs(nsqdHTTPAddrs); err != nil {
		log.Fatalf("--nsqd-http-address error - %s", err)
	}

	if err := checkAddrs(lookupdHTTPAddrs); err != nil {
		log.Fatalf("--lookupd-http-address error - %s", err)
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	go statLoop(intvl, *topic, *channel, nsqdHTTPAddrs, lookupdHTTPAddrs)

	<-termChan
}
