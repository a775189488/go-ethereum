package main

import (
	"flag"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"os"
	"time"
)

func main() {
	var (
		path = flag.String("path", "", "path of level db")
	)
	flag.Parse()

	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.TerminalFormat(false)))
	glogger.Verbosity(log.Lvl(log.LvlDebug))
	log.Root().SetHandler(glogger)

	db, err := enode.OpenDB(*path)
	if err != nil {
		utils.Fatalf("open db %s fail, err %v", *path, err)
	}
	if nodes := db.QuerySeeds(10, time.Hour); len(nodes) != 0 {
		for _, n := range nodes {
			log.Info(n.String())
		}
	} else {
		log.Info("empty level db")
	}
}
