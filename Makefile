build: *.go
	goimports -w *.go
	go build -o bidfta-scrape

clean:
	rm bidfta-scrape
