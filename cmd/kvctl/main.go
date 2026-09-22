// Command kvctl is a CLI client for a running raft-kv-store cluster.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"raftkv/kvstore"
	"raftkv/transport"
)

func main() {
	peersFlag := flag.String("peers", "", "comma-separated host:port list of all servers")
	flag.Parse()
	args := flag.Args()

	if *peersFlag == "" || len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kvctl -peers <host:port,...> get <key>")
		fmt.Fprintln(os.Stderr, "       kvctl -peers <host:port,...> put <key> <value>")
		fmt.Fprintln(os.Stderr, "       kvctl -peers <host:port,...> append <key> <value>")
		os.Exit(2)
	}

	var ends []transport.ClientEnd
	for _, addr := range strings.Split(*peersFlag, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		ends = append(ends, transport.NewTCPEnd(addr))
	}
	if len(ends) == 0 {
		fmt.Fprintln(os.Stderr, "no peers given")
		os.Exit(2)
	}

	ck := kvstore.MakeClerk(ends)

	switch args[0] {
	case "get":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: kvctl get <key>")
			os.Exit(2)
		}
		fmt.Println(ck.Get(args[1]))
	case "put":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: kvctl put <key> <value>")
			os.Exit(2)
		}
		ck.Put(args[1], args[2])
	case "append":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: kvctl append <key> <value>")
			os.Exit(2)
		}
		ck.Append(args[1], args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		os.Exit(2)
	}
}
