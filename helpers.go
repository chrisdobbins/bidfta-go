package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

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
func writeResults(filename string, contents []byte) {
	if len(contents) > 0 {
		ioutil.WriteFile(filename, contents, 0644)
	}
}

func genRand() (int, error) {
	bigNum, err := rand.Int(rand.Reader, big.NewInt(27121))
	if err != nil {
		fmt.Printf("genRand failed to create random number: %v", err)
		return 0, err
	}
	num := int(bigNum.Int64())
	return num + 15518, nil
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
	// tmp := []byte{}
	raw := []byte{}
	chromedp.Run(ctx,
		chromedp.Sleep(5*time.Second),
		chromedp.Navigate("https://www.bidfta.com/home"),
		// chromedp.EvaluateAsDevTools(`document.querySelector("body > div.ub-emb-container > div > div > div.ub-emb-scroll-wrapper > div.ub-emb-iframe-wrapper.ub-emb-visible > button").click();`, &tmp),
		chromedp.Sleep(2*time.Second),
		chromedp.Evaluate(getLocationsScript, &raw),
		chromedp.Sleep(5*time.Second),
	)
	json.Unmarshal(raw, &locationsMap)
	return locationsMap
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
