.PHONY: help build run

help:
	@printf "Available targets:\n"
	@printf "  help   Show this help message\n"
	@printf "  build  Build the itemchecklist binary\n"
	@printf "  run    Run the server with go run .\n"

build:
	go build -o itemchecklist .

run:
	go run .
