package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	_ "log"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	_ "github.com/chromedp/chromedp/kb"
)

// const onlyNew = true
const tzOffset string = "-05:00"
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
func writeResults(filename string, contents []byte) {
	if len(contents) > 0 {
		ioutil.WriteFile(filename, contents, 0644)
	}
}

func genRand() (int, error) {
	bigNum, err := rand.Int(rand.Reader, big.NewInt(17121))
	if err != nil {
		fmt.Printf("genRand failed to create random number: %v", err)
		return 0, err
	}
	num := int(bigNum.Int64())
	return num + 518, nil
}

const getLocationsScript string = `// top level: keys are state names, vals are objs
let locMap = Array.from(document.querySelectorAll("select#selectedLocationIds > optgroup")).reduce((accumulator, group) => { 
  if (!accumulator[group.label]) {
    accumulator[group.label] = {};
  }
  accumulator[group.label] = Array.from(group.children).reduce((acc2, opts) => {
    acc2[opts.innerText] = opts.value;
    return acc2
  }, {});
  return accumulator
  },
{}); locMap;`

func genLocationsMap(ctx context.Context) map[string]map[string]string {
	locationsMap := map[string]map[string]string{}
	tmp := []byte{}
	raw := []byte{}
	chromedp.Run(ctx,
		chromedp.Sleep(5*time.Second),
		chromedp.Navigate("https://www.bidfta.com/home"),
		chromedp.EvaluateAsDevTools(`document.querySelector("body > div.ub-emb-container > div > div > div.ub-emb-scroll-wrapper > div.ub-emb-iframe-wrapper.ub-emb-visible > button").click();`, &tmp),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(getLocationsScript, &raw),
	)
	json.Unmarshal(raw, &locationsMap)
	return locationsMap
}
func parseDayAndTime(unparsedDay, unparsedTime string) time.Time {
	timeFormatFilter := regexp.MustCompile(`\.[0-9]+$`)
	formattedTime := timeFormatFilter.ReplaceAllString(unparsedTime, "")
	timeToParse := fmt.Sprintf(`%sT%s%s`, unparsedDay, formattedTime, tzOffset)
	parsedEndTime, _ := time.Parse(time.RFC3339, timeToParse)
	return parsedEndTime
}
func getLocations(locations []string, allLocations map[string]map[string]string) []string {
	selectedLocationIDs := []string{}
	regexFilterTmpl := `(?i)\b%s\b`
	for _, loc := range locations {
		filter := regexp.MustCompile(fmt.Sprintf(regexFilterTmpl, loc))
		if _, ok := allLocations[loc]; ok {
			for _, id := range allLocations[loc] {
				selectedLocationIDs = append(selectedLocationIDs, id)
			}
		} else {
			for _, locationsInState := range allLocations {
				_, ok := locationsInState[loc]
				if ok {
					selectedLocationIDs = append(selectedLocationIDs, locationsInState[loc])
				} else {
					for city, locationID := range locationsInState {
						if filter.MatchString(city) {
							selectedLocationIDs = append(selectedLocationIDs, locationID)
						}
					}
				}
			}
		}
	}
	return selectedLocationIDs
}

type PickupDate struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func parsePickupDates(unparsedPickupDates []string) []PickupDate {
	pickupDates := []PickupDate{}
	const dateTimeFormatStr string = "January 2 2006 3:04 PM -07:00"
	// const dateFormatStr string = "January 2 2006"
	for _, pd := range unparsedPickupDates {
		pd = strings.ReplaceAll(pd, "•", "")
		filter := regexp.MustCompile(`([A-Za-z]+ [0-9]{1,2})(?:st|th|nd|rd), ([0-9]{4}) ([0-9]{1,2}:[0-9]{2} (?:AM|PM)) - ([0-9]{1,2}:[0-9]{2} (?:AM|PM))`)
		unparsedTimeStart := filter.ReplaceAllString(pd, "$1 $2 $3")
		timeStart, _ := time.Parse(dateTimeFormatStr, fmt.Sprintf("%s %s", unparsedTimeStart, tzOffset))
		unparsedTimeEnd := filter.ReplaceAllString(pd, "$1 $2 $4")
		timeEnd, _ := time.Parse(dateTimeFormatStr, fmt.Sprintf("%s %s", unparsedTimeEnd, tzOffset))
		pickupDate := PickupDate{
			Start: timeStart,
			End:   timeEnd,
		}
		pickupDates = append(pickupDates, pickupDate)
	}
	return pickupDates
}

func genLocationIDtoAuctionPrefixMap(ctx context.Context) map[string]string {
	tmp := []byte{}
	raw := []byte{}
	evalScript := `Array.from(document.querySelectorAll("select#selectedLocationIds > optgroup")).reduce((accumulator, v) => {
accumulator.push(...v.children); return accumulator;
}, []).map((v) => {return v.value});`
	chromedp.Run(ctx,
		chromedp.Sleep(5*time.Second),
		chromedp.Navigate("https://www.bidfta.com/home"),
		chromedp.EvaluateAsDevTools(`document.querySelector("body > div.ub-emb-container > div > div > div.ub-emb-scroll-wrapper > div.ub-emb-iframe-wrapper.ub-emb-visible > button").click();`, &tmp),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(evalScript, &raw),
	)
	jsonResult := []string{}
	json.Unmarshal(raw, &jsonResult)
	fmt.Println(jsonResult)
	retMap := map[string]string{}
	for _, locID := range jsonResult {
		rawResult := []byte{}
		chromedp.Run(ctx,
			chromedp.Navigate(fmt.Sprintf(`https://www.bidfta.com/home?&&selectedLocationIds=%s`, locID)),
			chromedp.Evaluate(`document.querySelector("p:nth-child(2)").innerText.replaceAll("Auction: ", "");`, &rawResult), //document.querySelector("p:nth-child(2)").innerText.replaceAll("Auction: ", "");
		)
		parsed := ""
		numFilter := regexp.MustCompile(`[0-9]+$`)
		json.Unmarshal(rawResult, &parsed)
		parsed = numFilter.ReplaceAllString(parsed, "")
		retMap[parsed] = locID
	}
	return retMap
}

func isWeekend(day time.Time) bool {
	switch day.Weekday() {
	case time.Saturday:
	case time.Sunday:
		return true
	}
	return false
}

type bidData struct {
	itemID    string
	bidderID  string
	auctionID string
	maxBid    float64
}

func bid(ctx context.Context, b bidData) {
	select {
	case <-ctx.Done():
		fmt.Println("context expired!")
		return
	default:
	}
	maxBid := fmt.Sprintf("%.2f", b.maxBid)
	auctionURL := fmt.Sprintf("https://www.bidfta.com/itemDetails?listView=true&idauctions=%s&idItems=%s", b.auctionID, b.itemID)
	bidderID := b.bidderID
	auctionID := b.auctionID
	itemID := b.itemID
	var currentBid string
	type Item struct {
		ID          int     `json:"id"`
		AuctionID   int     `json:"auctionId"`
		Qty         int     `json:"quantity"`
		IsClosed    bool    `json: "itemClosed"`
		ItemEndDate string  `json:"itemEndDateText"`
		ItemEndTime string  `json:"itemEndTimeText"`
		CurrentBid  float64 `json:"currentBid"`
		NextBid     float64 `json:"nextBid"`
		HighBidder  int     `json:"highBidder"`
		MaxBid      float64 `json:"maxBid"`
		SecureItem  bool    `json:"secureItem"`
	}
	type EndTimeResp struct {
		Items []Item `json:"items"`
	}
	getEndTimeFn := fmt.Sprintf(`async function getEndTime(){ const r = await fetch("https://www.bidfta.com/bidfta/getUpdateItems", {
   "headers": {'X-CSRF-Token': $("meta[name='_csrf']").attr("content"), "Accept": "application/json", "Content-Type": "application/json"
    },
   "referrerPolicy": "strict-origin-when-cross-origin",
   "body": "{\"idBidders\":\"%s\",\"idItems\":[%s],\"idauctions\":\"%s\"}",
   "method": "POST",
   "mode": "cors",
   "credentials": "include"
    }); return r.json()}; getEndTime()`, bidderID, itemID, auctionID)
	endTime := []byte{}
	resp := EndTimeResp{}
	chromedp.Run(ctx,
		chromedp.Sleep(7*time.Second),
		chromedp.Navigate(auctionURL),
		chromedp.Sleep(9*time.Second),
		chromedp.Evaluate(getEndTimeFn, &endTime, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
	)
	json.Unmarshal(endTime, &resp)
	fmt.Printf("Response: %+v\n", resp)

	itemEndTime := resp.Items[0].ItemEndTime
	itemEndDate := resp.Items[0].ItemEndDate
	// timeFormatFilter := regexp.MustCompile(`\.[0-9]+$`)
	// formattedTime := timeFormatFilter.ReplaceAllString(itemEndTime, "")
	// timeToParse := fmt.Sprintf("%sT%s%s", itemEndDate, formattedTime, tzOffset)
	// parsedEndTime, _ := time.Parse(time.RFC3339, timeToParse)
	parsedEndTime := parseDayAndTime(itemEndDate, itemEndTime)
	timeLeft := time.Until(parsedEndTime)
	fmt.Printf("time left: %v\n", timeLeft)

	if maxBid < currentBid {
		fmt.Println("current bid exceeds max bid")
		return
	}
	// if timeLeft.Hours() > float64(720*1.0) {
	// 	fmt.Printf("time left: %v\n", timeLeft)
	// 	return
	// }

	maxBidFn := fmt.Sprintf(`window.placeAjaxMaxBid(%s, %s, "%.2f", "%.2f")`, itemID, auctionID, resp.Items[0].CurrentBid, b.maxBid)

	tmp := []byte{}
	chromedp.Run(
		ctx,
		chromedp.Sleep(20*time.Second),
		chromedp.Evaluate(maxBidFn, &tmp),
		chromedp.Sleep(5777*time.Second),
	)
}

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
		chromedp.Run(ctx,
			chromedp.Sleep(1*time.Hour))
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
var locationOpts []string
var action string

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
	// flag.StringVar(&username, "username", defaultUsername, "login name")
	// flag.StringVar(&pw, "password", defaultPw, "password")
}

func getLocationAuctionPrefixMap() map[string]string {
	file, err := os.Open("loc-to-auc.json")
	if err != nil {
		fmt.Printf("failed to open file: %v\n", err)
		return make(map[string]string)
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		fmt.Printf("failed to open file: %v\n", err)
		return make(map[string]string)
	}
	mapping := make(map[string]string)
	json.Unmarshal(contents, &mapping)
	return mapping
}

func main() {
	username = os.Getenv("BIDFTA_USERNAME")
	userID := os.Getenv("BIDFTA_USERID")
	pw = os.Getenv("BIDFTA_PW")
	includeUsed = false
	id, _ := strconv.ParseInt(userID, 10, 32)
	bidderID = int(id)
	auctionsToLocations = make(map[string]string)

	flag.Parse()
	if _, ok := validActions[action]; !ok {
		log.Fatalf("invalid action: %s", action)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:2], chromedp.DefaultExecAllocatorOptions[3:]...)
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
		scrape(ctx, searchTerm)
	case "bid":
		// // bid on item:
		login(ctx)
		// TODO: make these cli options
		auctionID := "238578"
		itemID := "19919112"
		maxBid := 6.66
		d := bidData{
			auctionID: auctionID,
			bidderID:  "85357",
			itemID:    itemID,
			maxBid:    maxBid,
		}
		bid(ctx, d)
	case "watch":
		// find items:
		itemID := 20074699
		auctionID := 239673
		watch(context.Background(), auctionID, itemID)
	}
}
