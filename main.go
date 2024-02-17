package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Define a struct to match the JSON structure
type ApiResponse struct {
	RetCode int    `json:"ret_code"`
	RetMsg  string `json:"ret_msg"`
	Result  struct {
		List []struct {
			Symbol    string `json:"symbol"`
			Side      string `json:"side"`
			Timestamp string `json:"timestamp"`
			Value     string `json:"value"`
		} `json:"list"`
	} `json:"result"`
}

const apiURL = "https://api2.bybit.com/contract/v5/public/support/big-deal?limit=10&symbol=BTCUSDT"
const defaultFrequency = 1000 * time.Millisecond

var previousFirstRow int64
var minValueToDisplay int64 = 500000 // Default minimum value

func main() {
	bot, err := tgbotapi.NewBotAPI("") // Replace with your Telegram bot token
	if err != nil {
		fmt.Println("Error initializing bot:", err)
		os.Exit(1)
	}

	bot.Debug = true

	frequency := defaultFrequency
	fetchTicker := time.NewTicker(frequency)
	done := make(chan bool)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)
	if err != nil {
		fmt.Println("Error getting updates:", err)
		os.Exit(1)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		cachedChatID := update.Message.Chat.ID
		text := update.Message.Text

		if text == "Subscribe" {
			msg := tgbotapi.NewMessage(cachedChatID, "Bot started.")
			msg.ReplyMarkup = createMinValueButton() // Set the custom keyboard
			bot.Send(msg)

			{
				data, err := fetchData()
				if err != nil {
					fmt.Println("Error fetching data:", err)
					continue
				}

				if len(data) != 0 {
					msg := tgbotapi.NewMessage(cachedChatID, data)
					bot.Send(msg)
				}
			}

			// need parallel execution here for different tokens
			// for loop to iterate through every token, pass apiUrl to fetchData
			go func() {
				for {
					select {
					case <-done:
						return
					case <-fetchTicker.C:
						{
							data, err := fetchData()
							if err != nil {
								fmt.Println("Error fetching data:", err)
								continue
							}

							if len(data) != 0 {
								msg := tgbotapi.NewMessage(cachedChatID, data)
								bot.Send(msg)
							}
						}
					}
				}
			}()
		} else if text == "Unsubscribe" {
			fetchTicker.Stop()
			done <- true
			msg := tgbotapi.NewMessage(cachedChatID, "Bot stopped.")
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true) // Remove the custom keyboard
			bot.Send(msg)
		} else if strings.HasPrefix(text, "Show minimum displayed value $") {
			msg := tgbotapi.NewMessage(cachedChatID, fmt.Sprintf("Current minimum value: $ %d", minValueToDisplay))
			bot.Send(msg)
		} else if text == "Set minimum displayed value $" {
			msg := tgbotapi.NewMessage(cachedChatID, "Enter the new minimum value:")
			bot.Send(msg)
		} else if strings.HasPrefix(text, "Set minimum displayed value $") {
			newValueStr := strings.TrimPrefix(text, "Set minimum displayed value $")
			newValue, err := strconv.ParseInt(newValueStr, 10, 64)
			if err != nil {
				msg := tgbotapi.NewMessage(cachedChatID, "Invalid input. Please enter a valid number.")
				bot.Send(msg)
			} else {
				minValueToDisplay = newValue
				msg := tgbotapi.NewMessage(cachedChatID, fmt.Sprintf("Minimum value set to: $ %d", minValueToDisplay))
				bot.Send(msg)

				{
					data, err := fetchData()
					if err != nil {
						fmt.Println("Error fetching data:", err)
						continue
					}

					if len(data) != 0 {
						msg := tgbotapi.NewMessage(cachedChatID, data)
						bot.Send(msg)
					}
				}

			}
		}
	}
}

func createMinValueButton() tgbotapi.ReplyKeyboardMarkup {
	keyboard := [][]tgbotapi.KeyboardButton{
		{tgbotapi.NewKeyboardButton("Subscribe"), tgbotapi.NewKeyboardButton("Unsubscribe")},
		{tgbotapi.NewKeyboardButton("Set minimum displayed value $"), tgbotapi.NewKeyboardButton("Show minimum displayed value $")},
	}
	return tgbotapi.NewReplyKeyboard(keyboard...)
}

func fetchData() (string, error) {
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&result)
	if err != nil {
		return "", err
	}

	// DEBUG
	// fmt.Println("result:")
	// for key, value := range result {
	// 	fmt.Printf("%s: %v\n", key, value)
	// }

	// Extract the list of deals from the JSON
	deals, ok := result["result"].(map[string]interface{})["list"].([]interface{})
	if !ok {
		return "No data found", nil
	}

	firstElem := previousFirstRow

	// Format and store the deal information
	var formattedDeals []string
	for _, deal := range deals {
		if dealMap, ok := deal.(map[string]interface{}); ok {

			ivalue, err := strconv.ParseFloat(strings.Replace(dealMap["value"].(string), ",", "", -1), 64)
			if err != nil {
				panic(err)
			}
			value := int64(ivalue)

			// if value < 1000000 {
			// 	continue
			// }

			// Format the value with commas
			valueStr := strconv.FormatInt(value, 10)
			formattedValueWithCommas := addCommas(valueStr)

			// symbol := dealMap["symbol"].(string)
			itimestamp, err := strconv.ParseInt(dealMap["timestamp"].(string), 10, 64)
			if err != nil {
				panic(err)
			}

			if firstElem == itimestamp {
				break
			}

			timestamp := time.Unix(itimestamp, 0).Format("2006-01-02 15:04:05")

			side := dealMap["side"].(string)
			var emoji string
			if side == "Sell" {
				emoji = "❌" // Red "X" emoji for Sell
			} else if side == "Buy" {
				emoji = "✅" // Green checkmark emoji for Buy
			}
			formattedDeal := fmt.Sprintf("%s %s\t $ %s\n%s", emoji, side, formattedValueWithCommas, timestamp)

			if value > minValueToDisplay {
				formattedDeals = append(formattedDeals, formattedDeal)
			}

		}
	}

	if dealMap, ok := deals[0].(map[string]interface{}); ok {
		itimestamp, err := strconv.ParseInt(dealMap["timestamp"].(string), 10, 64)
		if err != nil {
			panic(err)
		}

		previousFirstRow = itimestamp
	}

	// Join the formatted deals into a single string
	response := strings.Join(formattedDeals, "\n")

	return response, nil
}

func addCommas(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	return addCommas(s[:n-3]) + "," + s[n-3:]
}
