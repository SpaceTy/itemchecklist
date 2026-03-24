.PHONY: help build run

help:
	@printf "Available targets:\n"
	@printf "  help   Show this help message\n"
	@printf "  build  Build the itemchecklist binary\n"
	@printf "  run    Run the server with go run ./src\n"

build:
	go build -o itemchecklist ./src

run:
	go run ./src
