package main

import "vision-relay/backend/internal/server"

func main() {
	if server.RunUpdateHelperIfRequested() {
		return
	}
	server.Run()
}
