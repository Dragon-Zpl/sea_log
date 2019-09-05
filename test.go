package main

import (
	"fmt"
	"time"
)
var i int
func main() {
	i = 1
	ch := make(chan *int)
	go func() {
		ch <- &i
	}()
	go func() {
		for   {
			select {
			case t :=<- ch:
				*t ++
			}
		}
	}()
	//time.Sleep(time.Duration(1)*time.Second)
	time.Sleep(time.Duration(1)*time.Second)
	fmt.Println(i)
}
