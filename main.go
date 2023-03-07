package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	_ "log"
	"math/big"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	_ "github.com/chromedp/chromedp/kb"
)

// const onlyNew = true
const getDataFnTmpl string = `async function getData(){ const r = await fetch("https://www.bidfta.com/bidfta/getUpdateItems", {
   "headers": {'X-CSRF-Token': $("meta[name='_csrf']").attr("content"), "Accept": "application/json", "Content-Type": "application/json"
    },
   "referrerPolicy": "strict-origin-when-cross-origin",
   "body": "{\"idBidders\":\"%d\",\"idItems\":%s,\"idauctions\":\"%s\"}",
   "method": "POST",
   "mode": "cors",
   "credentials": "include"
    }); return r.json()}; getData()`

type ItemDetails struct {
	AuctionID   string  `json:"auctionID"`
	ItemID      string  `json:"itemID"`
	Title       string  `json:"title"`
	LotSize     int     `json:"numOfItems"`
	LotCode     string  `json:"lotCode"`
	CurrentBid  float64 `json:"currentBid"`
	NextBid     float64 `json:"nextBid"`
	HighBidder  int     `json:"highBidder"`
	Condition   string  `json:"condition"`
	BidsEnabled bool    `json:"bidsEnabled"`
	EndTime     string  `json:"endTimeText"`
	EndDate     string  `json:"endDateText"`
	WeekendPickupAvailable bool `json:"weekendPickupAvailable"`
	PickupDates []time.Time `json:"pickupDates"`
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

func scrape(ctx context.Context, searchTerm string) {
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()
	login(ctx)
	locations := genLocationsMap(ctx)

	fmt.Printf("%+v\n", locations)
	testLoc := locations["Kentucky"]["Cynthiana"]
	testLoc2 := locations["Kentucky"]["Louisville - Shepherdsville Rd"]
	testLoc3 := locations["Kentucky"]["Georgetown - Lexington"]
	testLoc4 := locations["Ohio"]["Mansfield"]
	testLoc5 := locations["Illinois"]["IL, Germantown"]
	testLocs := []string{testLoc, testLoc2, testLoc3, testLoc4, testLoc5}
	testLocsEnc, _ := json.Marshal(testLocs)
	selectOptScript := fmt.Sprintf(`Array.from(document.querySelectorAll("option[value]")).filter((ov) => {return %s.indexOf(ov.value) > -1 }).forEach((v) => {v.selected = true}); window.filterAuctionFun('filterAuctions');`, string(testLocsEnc))
	tmp := []byte{}
	matchedItems := Items{}

	tmp = []byte{}
	delay, _ := genRand()
	fmt.Printf("delay of %+v\n", time.Duration(delay)*time.Millisecond)
	chromedp.Run(ctx,
		chromedp.Navigate("https://www.bidfta.com/home"),
		chromedp.Sleep(3000*time.Millisecond),
		chromedp.EvaluateAsDevTools(`document.querySelector("body > div.ub-emb-container > div > div > div.ub-emb-scroll-wrapper > div.ub-emb-iframe-wrapper.ub-emb-visible > button").click();`, &tmp))
	chromedp.Run(ctx,
		chromedp.Sleep(time.Duration(delay)*time.Millisecond),
		chromedp.WaitVisible("form#filterAuctions > div#location-optionClass-container", chromedp.ByQuery),
		chromedp.EvaluateAsDevTools(selectOptScript, &tmp),
		chromedp.Sleep(time.Duration(delay)*time.Millisecond),
	)

	defer func(matchedItems Items) {
		fileContents, err := json.MarshalIndent(matchedItems, "", " ")
		if err != nil {
			fmt.Println("failed to marshal json: ", err)
		}
		writeResults(fmt.Sprintf("%s.json", searchTerm), fileContents)
	}(matchedItems)

	delay, _ = genRand()
	fmt.Printf("delay of %+v\n", time.Duration(delay)*time.Millisecond)
	chromedp.Run(ctx,
		chromedp.Sleep(time.Duration(delay)*time.Millisecond),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector("form#searchAuctions > div.input-group > input#searchKeywords").value="%s"`, searchTerm), &tmp),
		chromedp.Sleep(415*time.Millisecond),
		chromedp.Click("#search-auction-button"),
		chromedp.Sleep(7*time.Second))

	totalPages := []byte{}
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("span.total.total_page").innerText`, &totalPages))
	totalNumOfPages, _ := strconv.Atoi(strings.Replace(string(totalPages), `"`, "", -1))

	// iter over auctions; get urls
	auctionNodes := []*cdp.Node{}
	for i := 1; i < totalNumOfPages+1; i++ {
		delay, _ = genRand()
		fmt.Printf("delay of %+v\n", time.Duration(delay)*time.Millisecond)
		chromedp.Run(ctx,
			chromedp.Sleep(time.Duration(delay)*time.Millisecond),
			chromedp.Nodes(".product-list > a", &auctionNodes, chromedp.ByQueryAll))
		auctionURLs := []string{}
		for _, an := range auctionNodes {
			auctionURLs = append(auctionURLs, an.AttributeValue("href"))
		}
		for _, au := range auctionURLs {
			delay, _ = genRand()
			fmt.Printf("delay of %+v\n", time.Duration(delay)*time.Millisecond)
			ctx, cancel := chromedp.NewContext(ctx)
			defer cancel()
			u, _ := url.Parse(au)
			params := u.Query()
			auctionID := params["idauctions"][0]
			resp := []byte{}
			endDate := ""
			pickupDates := []string{}
			chromedp.Run(ctx,
				chromedp.Navigate("https://www.bidfta.com"+au),
				chromedp.Sleep(time.Duration(delay)*time.Millisecond),
				// chromedp.WaitVisible(".col-lg-9 > div:nth-child(1n+13):not(:nth-child(1n+17))", chromedp.ByQueryAll),
				chromedp.Evaluate(`Array.from(document.querySelectorAll(".col-lg-9 > div:nth-child(1n+13):not(:nth-child(1n+17))")).map((e) => {return e.innerText});`, &pickupDates),
				chromedp.Evaluate(`document.querySelector("aside.contact-info").innerText.split("\n\n")[1]`, &endDate),
				chromedp.Click("a.bidDetails"),
				chromedp.Sleep(777*time.Millisecond),
				chromedp.SetValue("itemSearchKeywords", searchTerm, chromedp.ByID),
				chromedp.Evaluate(`window.searchItemsList()`, &resp),
				chromedp.Sleep(7*time.Second),
			)

			tmp := []byte{}
			possibleMatchObjs := []byte{}
			chromedp.Run(ctx,
				chromedp.Poll(`("idItems" in window) && window.idItems.length > 0`, &tmp),
				chromedp.Evaluate(`window.idItems`, &possibleMatchObjs),
			)
			weekendPickupAvailable := false
			for _, pd := range pickupDates {
				pd = strings.ReplaceAll(pd, "•", "")
				filter := regexp.MustCompile(`([A-Za-z]+ [0-9]{1,2})(?:st|th|nd|rd), ([0-9]{4}) ([0-9]{1,2}:[0-9]{2} (?:AM|PM)) - ([0-9]{1,2}:[0-9]{2} (?:AM|PM))`)
				dayYear := filter.ReplaceAllString(pd, "$1 $2")
				fullDay, _ := time.Parse("January 2 2006", dayYear)
				if isWeekend(fullDay) {
					weekendPickupAvailable = true
				}
			}
			possMatches := []int{}
			json.Unmarshal(possibleMatchObjs, &possMatches)
			itemIDs := ""
			for idx, p := range possMatches {
				itemIDs += fmt.Sprintf("%d", p)
				if idx < len(possMatches)-2 {
					itemIDs += ","
				}
			}
			type ResponseItem struct {
				ID          int     `json:"id"`
				Quantity    int     `json:"quantity"`
				CurrentBid  float64 `json:"currentBid"`
				NextBid     float64 `json:"nextBid"`
				HighBidder  int     `json:"highBidder"`
				BidsCount   int     `json:"bidsCount"`
				EndDate     string  `json:"itemEndDateText"`
				EndTime     string  `json:"itemEndTimeText"`
				BidsEnabled bool    `json:"bidsEnabled"`
			}
			type Response struct {
				Items []ResponseItem `json:"items"`
			}
			totalNumOfItemPages := ""
			chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelector("span.total.total_page").innerText`, &totalNumOfItemPages),
			)
			itemPages, _ := strconv.Atoi(strings.Replace(string(totalNumOfItemPages), `"`, "", -1))
			for currItemPage := 0; currItemPage < itemPages; currItemPage++ {
				func(currPage int) {

					// search auctions for item
					possibleMatchNodes := []*cdp.Node{}
					delay, _ := genRand()
					fmt.Printf("delay of %+v\n", time.Duration(delay)*time.Millisecond)
					chromedp.Run(ctx,
						chromedp.Sleep(time.Duration(delay)*time.Millisecond),
						chromedp.Nodes("div.product-list", &possibleMatchNodes, chromedp.ByQueryAll),
					)
					reqItems := []int{}
					for idx, pmn := range possibleMatchNodes {
						title := ""
						itemPath := ""
						pathExists := false
						lotCode := ""
						condition := ""
						chromedp.Run(ctx,
							chromedp.Text("p.itemStatus", &condition, chromedp.ByQuery, chromedp.FromNode(possibleMatchNodes[idx])),
							chromedp.Text("p.title", &title, chromedp.ByQuery, chromedp.FromNode(pmn)),
							chromedp.AttributeValue("a", "href", &itemPath, &pathExists, chromedp.ByQuery, chromedp.FromNode(pmn)),
							chromedp.Text("p.brandCode", &lotCode, chromedp.ByQuery, chromedp.FromNode(pmn)),
						)
						itemURL, _ := url.Parse(itemPath)
						itemURLParams := itemURL.Query()
						itemID := itemURLParams["idItems"][0] // note: to go to an item's page (the path is the href attribute of the any of these nodes: document.querySelectorAll("div.product-list.listView  a");):
						// window.getMainpage("/itemDetails?listView=true&pageId=1&idauctions=230243&idItems=19228423&firstIdItem=19228423&source=auctionItems&lastIdItem=19228542&itemSearchKeywords=")
						conditionFilter := regexp.MustCompile(`(?i)new`)
						if !conditionFilter.Match([]byte(condition)) {
							continue
						}

						searchTermFilter := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s\b`, searchTerm))
						if searchTermFilter.Match([]byte(title)) {
							if _, ok := matchedItems[auctionID]; !ok {
								matchedItems[auctionID] = map[string]ItemDetails{}
							} 
							detail := ItemDetails{}
							detail.Title = title
							detail.LotCode = lotCode
							detail.AuctionID = auctionID
							detail.ItemID = itemID
							reqItem, _ := strconv.Atoi(itemID)
							reqItems = append(reqItems, reqItem)
							detail.Condition = condition
							detail.WeekendPickupAvailable = weekendPickupAvailable
							matchedItems[auctionID][itemID] = detail
						}
					}
					if len(reqItems) == 0 {
						return
					}
					apiRespRaw := []byte{}
					apiResp := Response{}
					itemIDsEnc, _ := json.Marshal(reqItems)
					getDataFn := fmt.Sprintf(getDataFnTmpl, bidderID, string(itemIDsEnc), auctionID)
					chromedp.Run(ctx,
						chromedp.Sleep(12*time.Second),
						chromedp.Evaluate(getDataFn, &apiRespRaw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
							return p.WithAwaitPromise(true)
						}),
					)
					json.Unmarshal(apiRespRaw, &apiResp)
					for _, respItem := range apiResp.Items {
						itemIDkey := fmt.Sprintf("%d", respItem.ID)
						if _, ok := matchedItems[auctionID][itemIDkey]; ok {
							newItem := matchedItems[auctionID][itemIDkey]
						newItem.LotSize = respItem.Quantity
							newItem.CurrentBid = respItem.CurrentBid
							newItem.NextBid = respItem.NextBid
							newItem.HighBidder = respItem.HighBidder
							newItem.EndDate = respItem.EndDate
							newItem.EndTime = respItem.EndTime
							matchedItems[auctionID][itemIDkey] = newItem
						}
					}
				}(currItemPage)
				if currItemPage == 0 && itemPages == 1 {
					break
				}
			}
			cancel()
		}
		resp := []byte{}
		if i > totalNumOfPages-1 {
			break
		}
		fmt.Printf("changing to page %d of %d auction pages\n", i+1, totalNumOfPages)
		chromedp.Run(ctx,
			chromedp.Sleep(2*time.Second),
			chromedp.Evaluate(fmt.Sprintf(`window.pageChange(%d)`, i), &resp),
			chromedp.Sleep(9*time.Second),
		)

	}

	cancel()

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
	tzOffset := "-05:00"
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
	timeFormatFilter := regexp.MustCompile(`\.[0-9]+$`)
	formattedTime := timeFormatFilter.ReplaceAllString(itemEndTime, "")
	timeToParse := fmt.Sprintf("%sT%s%s", itemEndDate, formattedTime, tzOffset)
	parsedEndTime, _ := time.Parse(time.RFC3339, timeToParse)
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
	flag.StringVar(&searchTerm, "search-term", defaultSearchTerm, "item to search for")
	flag.StringVar(&action, "action", defaultAction, "action to take. one of: 'scrape', 'bid', watch'")
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

	flag.Parse()

	if _, ok := validActions[action]; !ok {
		log.Fatalf("invalid action: %s", action)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:2], chromedp.DefaultExecAllocatorOptions[3:]...)
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	switch action {
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
