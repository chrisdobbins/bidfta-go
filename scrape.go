package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// how to use jq to get weekend pickups: jq '. | to_entries | .[] | .value | to_entries | .[] | select(.value.weekendPickupAvailable == true)' generator.json
func scrape(ctx context.Context, searchTerm string, locations []string) {
	fmt.Println("scraping")
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()
	login(ctx)
	locationsMap := genLocationsMap(ctx)

	idsToLocations := map[string]string{}
	for _, warehouses := range locationsMap {
		for warehouse, id := range warehouses {
			idsToLocations[id] = warehouse
		}
	}
	fmt.Printf("%+v\n", locationsMap)
	selectedLocations := []string{"Kentucky", "Illinois", "Ohio"} //:= getLocations([]string{"Kentucky", "Illinois", "Ohio"}, locationsMap)
	if len(locations) > 0 {
		selectedLocations = locations
	}
	locs := getLocations(selectedLocations, locationsMap)
	testLocsEnc, _ := json.Marshal(locs)
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
		chromedp.Sleep(3000*time.Millisecond),
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
			auctionPrefix := ""
			chromedp.Run(ctx,
				chromedp.Navigate("https://www.bidfta.com"+au),
				chromedp.Sleep(time.Duration(delay)*time.Millisecond),
				// chromedp.WaitVisible(".col-lg-9 > div:nth-child(1n+13):not(:nth-child(1n+17))", chromedp.ByQueryAll),
				chromedp.Evaluate(`Array.from(document.querySelectorAll(".col-lg-9 > div:nth-child(1n+13):not(:nth-child(1n+17))")).map((e) => {return e.innerText});`, &pickupDates),
				chromedp.Evaluate(`var f2 = /(Auction:\s+|[0-9]+$)/g; document.querySelectorAll("h2")[0].innerText.replaceAll(f2, "");`, &auctionPrefix),
				chromedp.Evaluate(`document.querySelector("aside.contact-info").innerText.split("\n\n")[1]`, &endDate),
				chromedp.Click("a.bidDetails"),
				chromedp.Sleep(777*time.Millisecond),
				chromedp.SetValue("itemSearchKeywords", searchTerm, chromedp.ByID),
				chromedp.Evaluate(`window.searchItemsList()`, &resp),
				chromedp.Sleep(7*time.Second),
			)
			fmt.Println("auction: ", auctionPrefix)

			tmp := []byte{}
			possibleMatchObjs := []byte{}
			chromedp.Run(ctx,
				chromedp.Poll(`("idItems" in window) && window.idItems.length > 0`, &tmp),
				chromedp.Evaluate(`window.idItems`, &possibleMatchObjs),
			)
			weekendPickupAvailable := false

			pickupDateTimes := parsePickupDates(pickupDates)
			for _, pickupDateTime := range pickupDateTimes {
				if isWeekend(pickupDateTime.Start) {
					weekendPickupAvailable = true
					break
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

						locationID := ""
						searchTermFilter := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s\b`, searchTerm))
						if searchTermFilter.Match([]byte(title)) {
							if _, ok := matchedItems[auctionID]; !ok {
								matchedItems[auctionID] = map[string]ItemDetails{}
							}
							if v, ok := auctionsToLocations[auctionPrefix]; !ok {
								fmt.Printf("WARN: failed to get location ID for %s\n", auctionPrefix)
							} else {
								locationID = v
							}
							location, ok := idsToLocations[locationID]
							if !ok {
								fmt.Printf("WARN: location not found for %+v in the map\n", locationID)
							}
							fmt.Println("location: ", location)
							detail := ItemDetails{}
							detail.Location = location
							detail.Title = title
							detail.LotCode = lotCode
							detail.AuctionID = auctionID
							detail.ItemID = itemID
							reqItem, _ := strconv.Atoi(itemID)
							reqItems = append(reqItems, reqItem)
							detail.Condition = condition
							detail.WeekendPickupAvailable = weekendPickupAvailable
							detail.PickupDates = pickupDateTimes
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
							newItem.BidsEnabled = respItem.BidsEnabled
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
