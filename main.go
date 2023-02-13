package main

import (
	"context"
	"fmt"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	_ "github.com/chromedp/chromedp/kb"
	"log"
	"strconv"
	"strings"
	"time"
)

func main() {
	var nodes []*cdp.Node

	opts := append(chromedp.DefaultExecAllocatorOptions[:2], chromedp.DefaultExecAllocatorOptions[3:]...)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	ctx, cancel = chromedp.NewExecAllocator(ctx, opts...)
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()
	var nodes2 []*cdp.Node
	chromedp.Run(ctx,
		chromedp.Navigate("https://www.bidfta.com/home"),
		chromedp.WaitVisible("form#filterAuctions > div#location-optionClass-container"),
		chromedp.Click("form#filterAuctions > div#location-optionClass-container"),
		chromedp.WaitVisible("ul.multiselect-container.dropdown-menu > li.multiselect-group"),
		chromedp.Nodes("ul.multiselect-container.dropdown-menu > li.multiselect-group", &nodes, chromedp.ByQueryAll),
		chromedp.Nodes("ul.multiselect-container.dropdown-menu > li.multiselect-group label > b", &nodes2, chromedp.ByQueryAll),
	)
	locs := map[string]struct{}{}
	checkboxesByLoc := make(map[string]*cdp.Node)
	for _, n := range nodes2 {
		loc := ""
		chromedp.Run(ctx, chromedp.Text(n.FullXPath(), &loc, chromedp.BySearch))
		loc = strings.TrimSpace(loc)
		locs[loc] = struct{}{}
		var nodes3 []*cdp.Node
		chromedp.Run(ctx, chromedp.Nodes(n.FullXPath()+"/preceding-sibling::input", &nodes3, chromedp.BySearch))
		locNode := nodes3[0]
		checkboxesByLoc[loc] = new(cdp.Node)
		checkboxesByLoc[loc] = locNode
	}

	var res []byte
	chromedp.Run(ctx, chromedp.Click(checkboxesByLoc["Kentucky"].FullXPath()), chromedp.Click(checkboxesByLoc["Illinois"].FullXPath()), chromedp.Sleep(17*time.Second), chromedp.Evaluate(`document.querySelector('input[type="button"]').click()`, &res), chromedp.Sleep(20*time.Second))
	fmt.Println("clicked button")

	chromedp.Run(ctx, chromedp.Sleep(20*time.Second), chromedp.Evaluate(`document.querySelector("form#searchAuctions > div.input-group > input#searchKeywords").value="kettlebell"`, &res), chromedp.Sleep(17*time.Second), chromedp.Click("#search-auction-button"), chromedp.Sleep(12*time.Second))

	totalPages := []byte{}
	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("span.total.total_page").innerText`, &totalPages))
	totalNumOfPages, err := strconv.Atoi(strings.Replace(string(totalPages), `"`, "", -1))
	if err != nil {
		log.Fatalf("failed to get num of pages: %v", err)
	}
	fmt.Println("total pages:", totalNumOfPages)

	// iter over auctions; get urls
	auctionNodes := []*cdp.Node{}
	for i := 0; i < totalNumOfPages; i++ {
		chromedp.Run(ctx, chromedp.Nodes(".product-list > a", &auctionNodes, chromedp.ByQueryAll))
		auctionURLs := []string{}
		for _, an := range auctionNodes {
			auctionURLs = append(auctionURLs, an.AttributeValue("href"))
		}
		fmt.Println(auctionURLs)
		for _, au := range auctionURLs {
			// info := ""
			func() {
				tmpNodes := []*cdp.Node{}
				ctx, cancel := chromedp.NewContext(ctx)
				defer cancel()
	                        totalPages := []byte{}
				chromedp.Run(ctx,
					chromedp.Navigate("https://www.bidfta.com"+au),
					chromedp.Click("a.bidDetails"),
					chromedp.Sleep(19*time.Second),
					chromedp.Nodes("itemSearchKeywords", &tmpNodes, chromedp.ByID),
                                        chromedp.Sleep(22*time.Second),
                                	chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("span.total.total_page").innerText`, &totalPages)),
				)
                                      	totalNumOfPages, err := strconv.Atoi(strings.Replace(string(totalPages), `"`, "", -1))
                                  	if err != nil {
                           	        	log.Fatalf("failed to get num of pages: %v", err)
	                                }
                                
                                // search auctions for item
				resp := []byte{}
                                //possibleMatchNodes := []*cdp.Node{}
				chromedp.Run(ctx,
					chromedp.Evaluate(`document.querySelector('input[type="button"]').click()`, &resp),
					chromedp.SetValue("itemSearchKeywords", "kettlebell", chromedp.ByID),
					chromedp.Sleep(5*time.Second),
					chromedp.Sleep(15*time.Second),
		        		chromedp.Evaluate(`window.searchItemsList()`, &resp),
					chromedp.Sleep(70*time.Second),
                                        chromedp.Evaluate(fmt.Sprintf(`window.pageChange(%d)`, j), &resp),
				)
                            }
			}()
                for j := 0; j < totalNumOfPages; j++ {
		}
		resp := []byte{}
		chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`window.pageChange(%d)`, i), &resp),
			chromedp.Sleep(27*time.Second),
		)
	}
}
