package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	_ "log"
	"os"
	"strconv"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	_ "github.com/chromedp/chromedp/kb"
)

// const onlyNew = true
const tzOffset string = "-04:00"
const getDataFnTmpl string = `async function getData(){ const r = await fetch("https://www.bidfta.com/bidfta/getUpdateItems", {
   "headers": {'X-CSRF-Token': $("meta[name='_csrf']").attr("content"), "Accept": "application/json", "Content-Type": "application/json"
    },
   "referrerPolicy": "strict-origin-when-cross-origin",
   "body": "{\"idBidders\":\"%d\",\"idItems\":%s,\"idauctions\":\"%s\"}",
   "method": "POST",
   "mode": "cors",
   "credentials": "include"
    }); return r.json()}; getData()`

var auctionsToLocations map[string]string

type ItemDetails struct {
	AuctionID              string       `json:"auctionID"`
	ItemID                 string       `json:"itemID"`
	Title                  string       `json:"title"`
	LotSize                int          `json:"numOfItems"`
	LotCode                string       `json:"lotCode"`
	CurrentBid             float64      `json:"currentBid"`
	NextBid                float64      `json:"nextBid"`
	HighBidder             int          `json:"highBidder"`
	Condition              string       `json:"condition"`
	BidsEnabled            bool         `json:"bidsEnabled"`
	EndTime                string       `json:"endTimeText"`
	EndDate                string       `json:"endDateText"`
	WeekendPickupAvailable bool         `json:"weekendPickupAvailable"`
	PickupDates            []PickupDate `json:"pickupDates"`
	Location               string       `json:"location"`
}
type Items map[string]map[string]ItemDetails // mapping of auction IDs to a mapping of item IDs to matched items

func watch(ctx context.Context, auctionID, itemID int) {
	getDataFn := fmt.Sprintf(`async function getData(){ const r = await fetch("https://www.bidfta.com/bidfta/getUpdateItems", {
   "headers": {'X-CSRF-Token': $("meta[name='_csrf']").attr("content"), "Accept": "application/json", "Content-Type": "application/json"
    },
   "referrerPolicy": "strict-origin-when-cross-origin",
   "body": "{\"idBidders\":\"%d\",\"idItems\":[%d],\"idauctions\":\"%d\"}",
   "method": "POST",
   "mode": "cors",
   "credentials": "include"
    }); return r.json()}; getData()`, bidderID, itemID, auctionID)
	type ResponseItem struct {
		ID         int     `json:"id"`
		Quantity   int     `json:"quantity"`
		CurrentBid float64 `json:"currentBid"`
		NextBid    float64 `json:"nextBid"`
		HighBidder int     `json:"highBidder"`
		EndDate    string  `json:"itemEndDateText"`
		EndTime    string  `json:"itemEndTimeText"`
	}
	type Response struct {
		Items []ResponseItem `json:"items"`
	}

	ctx, cancel := chromedp.NewExecAllocator(ctx)
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()
	login(ctx)
	for {
		respRaw := []byte{}
		resp := Response{}
		chromedp.Run(ctx,
			chromedp.Navigate(fmt.Sprintf("https://www.bidfta.com/itemDetails?listView=false&idauctions=%d&idItems=%d", auctionID, itemID)),
			chromedp.Sleep(10*time.Second),
			chromedp.Evaluate(getDataFn, &respRaw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
				return p.WithAwaitPromise(true)
			}),
		)
		json.Unmarshal(respRaw, &resp)
		fmt.Printf("response: %+v\n", resp)
		time.Sleep(5 * time.Minute)
	}
}

func login(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	tmp := []byte{}
	pg := "https://www.bidfta.com/login"
	chromedp.Run(ctx,
		chromedp.Navigate(pg),
		chromedp.Sleep(2*time.Second),
		chromedp.SetValue("username", username, chromedp.ByID),
		chromedp.SetValue("password", pw, chromedp.ByID),
		chromedp.Sleep(7*time.Second),
		chromedp.Evaluate(`document.querySelector("form[name='loginForm']").submit()`, &tmp),
		chromedp.Sleep(10*time.Second),
	)
	return
	// return ctx
}

var searchTerm string
var username string
var pw string
var includeUsed bool
var bidderID int
var validActions map[string]struct{}

// var locationOpts []string
var action string
var auctionID string
var itemID string
var maxBid float64
var selectedLocation string
var headless bool
var now bool // bidding now rather than waiting...mostly used for debugging

func init() {
	const (
		defaultSearchTerm = "bookshelf"
		defaultAction     = "scrape"
	)
	validActions = map[string]struct{}{}
	validActions["scrape"] = struct{}{}
	validActions["bid"] = struct{}{}
	validActions["watch"] = struct{}{}
	validActions["generate-map"] = struct{}{} // this generates the mapping between location IDs and individual auctions by grabbing the first few letters of a sample auction for each location and writing them to loc-to-auc.json
	flag.StringVar(&searchTerm, "search-term", defaultSearchTerm, "item to search for")
	flag.StringVar(&action, "action", defaultAction, "action to take. one of: 'scrape', 'bid', watch', 'generate-map'")
	flag.StringVar(&auctionID, "auction-id", "", "auction ID")
	flag.StringVar(&itemID, "item-id", "", "item ID")
	flag.Float64Var(&maxBid, "max-bid", 0.00, "max bid")
	flag.StringVar(&selectedLocation, "location", "", "locations to search")
	flag.BoolVar(&headless, "headless", false, "headless mode (default: false)")
	flag.BoolVar(&now, "now", false, "bid now (default: false)")
	// flag.StringVar(&username, "username", defaultUsername, "login name")
	// flag.StringVar(&pw, "password", defaultPw, "password")
}

func main() {
	username = os.Getenv("BIDFTA_USERNAME")
	userID := os.Getenv("BIDFTA_USERID")
	pw = os.Getenv("BIDFTA_PW")
	includeUsed = false
	id, _ := strconv.ParseInt(userID, 10, 32)
	bidderID = int(id)
	auctionsToLocations = make(map[string]string)

	// fmt.Println(calcPollInterval(50 * time.Hour))
	// var lastUpdateTime *time.Time
	// if lastUpdateTime == nil {
	// 	lastUpdateTime = &time.Time{}
	// 	*(lastUpdateTime) = time.Now()
	// }
	// fmt.Println(lastUpdateTime)
	// time.Sleep(20 * time.Hour)
	flag.Parse()
	if _, ok := validActions[action]; !ok {
		log.Fatalf("invalid action: %s", action)
	}
	var opts []func(*chromedp.ExecAllocator)
	fmt.Println("headless: ", headless)
	// time.Sleep(2 * time.Hour)
	if headless {
		opts = chromedp.DefaultExecAllocatorOptions[:]
	} else {
		opts = chromedp.DefaultExecAllocatorOptions[:2]
		opts = append(opts, chromedp.DefaultExecAllocatorOptions[3:]...) // don't run headlessly
	}
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()
	auctionsToLocations = getLocationAuctionPrefixMap()
	fmt.Println(auctionsToLocations)

	switch action {
	case "generate-map":
		locAucIDMap := genLocationIDtoAuctionPrefixMap(ctx)
		fileContents, _ := json.Marshal(locAucIDMap)
		writeResults("loc-to-auc.json", fileContents)
	case "scrape":
		scrape(ctx, searchTerm, []string{selectedLocation})
	case "bid":
		// // bid on item:
		defer cancel()
		fmt.Println(maxBid)
		login(ctx)
		d := bidData{
			auctionID: auctionID,
			bidderID:  fmt.Sprintf("%d", bidderID),
			itemID:    itemID,
			maxBid:    maxBid,
		}
		fmt.Println(d)
		bid(ctx, d)
	case "watch":
		// find items:
		// itemID := 20074699
		// auctionID := 239673
		auctionID, _ := strconv.Atoi(auctionID)
		itemID, _ := strconv.Atoi(itemID)
		watch(context.Background(), auctionID, itemID)
	}
}
