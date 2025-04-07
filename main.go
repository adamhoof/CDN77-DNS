package main

import (
	"CDN77-DNS/naive"
	"CDN77-DNS/optimised"
	"fmt"
	"net"
)

func main() {
	nd := naive.Data{}
	err := nd.LoadRoutingData("routing-data.txt")
	if err != nil {
		fmt.Println(err)
	}

	d := optimised.NewData()
	err = d.LoadRoutingData("routing-data.txt")
	if err != nil {
		fmt.Println(err)
	}

	_, testNet, _ := net.ParseCIDR("2001:49f0:d0b8:aaaa::/56")
	pop, scpl := nd.Route(testNet)

	pop, scpl = d.Route(testNet)

	fmt.Printf("Pop: %+v, Scope prefix length: %+v \n", pop, scpl)
}
