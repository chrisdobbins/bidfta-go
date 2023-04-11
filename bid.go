package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type bidData struct {
	itemID    string
	bidderID  string
	auctionID string
	maxBid    float64
}
type Item struct {
	ID          int     `json:"id"`
	AuctionID   int     `json:"auctionId"`
	Qty         int     `json:"quantity"`
	IsClosed    bool    `json:"itemClosed"`
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
	Msg   string `json:"msg,omitempty"`
}

// func (r *EndTimeResp) UnmarshalJSON(b []byte) error {
// 	type Custom struct {
// 		ID          int     `json:"id"`
// 		AuctionID   int     `json:"auctionId"`
// 		Qty         int     `json:"quantity"`
// 		IsClosed    bool    `json:"itemClosed"`
// 		ItemEndDate string  `json:"itemEndDateText"`
// 		ItemEndTime string  `json:"itemEndTimeText"`
// 		CurrentBid  float64 `json:"currentBid"`
// 		NextBid     float64 `json:"nextBid"`
// 		HighBidder  int     `json:"highBidder"`
// 		MaxBid      float64 `json:"maxBid"`
// 		SecureItem  bool    `json:"secureItem"`
// 	}
// 	// r := &EndTimeResp{}

// 	fmt.Println(string(b))
// 	rawString := string(b)
// 	currentBidFilter := regexp.MustCompile(`("currentBid":)([1-9]+)(?:,)`)
// 	rawString = currentBidFilter.ReplaceAllString(rawString, `$1$2.00,`)
// 	nextBidFilter := regexp.MustCompile(`("nextBid":)([1-9]+)(?:,)`)
// 	rawString = nextBidFilter.ReplaceAllString(rawString, `$1$2.00,`)
// 	b = []byte(rawString)
// 	type CustomResponse struct {
// 		Items []Custom
// 	}
// 	items := []Item{}
// 	customRes := CustomResponse{}
// 	customRes2 := CustomResponse{}
// 	json.Unmarshal(b, &customRes2)
// 	fmt.Println(customRes2)
// 	time.Sleep(2 * time.Hour)
// 	json.Unmarshal(b, &customRes)
// 	customObjs := customRes.Items

// 	for _, c := range customObjs {
// 		items = append(items, Item{
// 			CurrentBid:  c.CurrentBid,
// 			NextBid:     c.NextBid,
// 			AuctionID:   c.AuctionID,
// 			Qty:         c.Qty,
// 			IsClosed:    c.IsClosed,
// 			ItemEndDate: c.ItemEndDate,
// 			ItemEndTime: c.ItemEndTime,
// 			HighBidder:  c.HighBidder,
// 			MaxBid:      c.MaxBid,
// 			SecureItem:  c.SecureItem,
// 			ID:          c.ID,
// 		})
// 	}
// 	r.Items = items
// 	return nil
// }

func calcPollInterval(timeLeft time.Duration) time.Duration {
	// switch {
	// case timeLeft.Hours() >= 24.00:
	// 	return 12
	// case timeLeft.Hours() >= 12.00:
	// 	return 8
	// case timeLeft.Hours() >= 8.00:
	// 	return 4
	// case timeLeft.Hours() >= 4.00:

	// }

	if timeLeft.Hours() >= 24.0 {
		return 8 * time.Hour
	}
	if timeLeft.Hours() >= 12.0 {
		return 4 * time.Hour
		// return 8
	}
	if timeLeft.Minutes() > 4.20 {
		return timeLeft / 4
	}
	return 2500 * time.Millisecond
}

func bid(ctx context.Context, b bidData) {
	var timeLeft time.Duration
	var endTime time.Time
	// maxBid := fmt.Sprintf("%.2f", b.maxBid)
	auctionURL := fmt.Sprintf("https://www.bidfta.com/itemDetails?listView=true&idauctions=%s&idItems=%s", b.auctionID, b.itemID)
	bidderID := b.bidderID
	auctionID := b.auctionID
	itemID := b.itemID

	getEndTimeFn := fmt.Sprintf(`async function getEndTime(){ const r = await fetch("https://www.bidfta.com/bidfta/getUpdateItems", {
   "headers": {'X-CSRF-Token': $("meta[name='_csrf']").attr("content"), "Accept": "application/json", "Content-Type": "application/json"
    },
   "referrerPolicy": "strict-origin-when-cross-origin",
   "body": "{\"idBidders\":\"%s\",\"idItems\":[%s],\"idauctions\":\"%s\"}",
   "method": "POST",
   "mode": "cors",
   "credentials": "include"
    }); return r.json()}; getEndTime()`, bidderID, itemID, auctionID)
	chromedp.Run(ctx,
		chromedp.Sleep(7*time.Second),
		chromedp.Navigate(auctionURL),
		chromedp.Sleep(19*time.Second),
	)
	// var lastUpdateTime *time.Time
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	var emptyTime time.Time
	for {
		select {
		case <-ctx.Done():
			fmt.Println("context expired!")
			return
		case <-ticker.C:

			fmt.Println("new tick")
			endTimeRaw := []byte{}
			resp := EndTimeResp{}
			chromedp.Run(ctx,
				chromedp.Sleep(7*time.Second),
				chromedp.Evaluate(getEndTimeFn, &endTimeRaw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
					return p.WithAwaitPromise(true)
				}),
			)
			json.Unmarshal(endTimeRaw, &resp)
			fmt.Printf("Response: %+v\n", resp)

			itemEndTime := resp.Items[0].ItemEndTime
			itemEndDate := resp.Items[0].ItemEndDate
			// timeFormatFilter := regexp.MustCompile(`\.[0-9]+$`)
			// formattedTime := timeFormatFilter.ReplaceAllString(itemEndTime, "")
			// timeToParse := fmt.Sprintf("%sT%s%s", itemEndDate, formattedTime, tzOffset)
			// parsedEndTime, _ := time.Parse(time.RFC3339, timeToParse)
			endTime = parseDayAndTime(itemEndDate, itemEndTime)
			if endTime == emptyTime {
				continue
			}
			timeLeft = time.Until(endTime)
			fmt.Printf("time left: %v\n", timeLeft)
			if timeLeft < 0*time.Millisecond {
				return
			}

			currentBid := resp.Items[0].CurrentBid
			if maxBid < currentBid {
				fmt.Println("current bid exceeds max bid")
				return
			}
			fmt.Println(timeLeft)
			if !now && timeLeft >= 73000*time.Millisecond {
				nextInterval := calcPollInterval(timeLeft)
				fmt.Printf("next poll cycle at: %v\n", time.Now().Add(nextInterval))
				ticker.Reset(nextInterval)
				// ticker.Reset(3 * time.Minute)
				continue
			}

			maxBidFn := fmt.Sprintf(`window.placeAjaxMaxBid(%s, %s, "%.2f", "%.2f")`, itemID, auctionID, currentBid, b.maxBid)

			fmt.Println(maxBidFn)
			tmp := []byte{}
			chromedp.Run(
				ctx,
				chromedp.Sleep(100*time.Millisecond),
				chromedp.Evaluate(maxBidFn, &tmp),
				chromedp.Sleep(5777*time.Second),
			)
			return
			// default:
			// return
			// continue
		}
	}
}
