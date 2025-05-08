package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"macd/utils"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
	"golang.org/x/sync/semaphore"
)

type CoinIndicator struct {
	Symbol       string  //代币名称
	Price        float64 //代币价格
	TimeInternal string  //代币时段
	MA25         float64 //MA25
	MACD         float64 //MACD
	Signal       float64 //信号?
	Histogram    float64 //MACD柱子
	Volume24H    float64 // 24小时交易量
	StopLossRate float64 // 止损百分比
}

var (
	apiKey          = ""                       // 在此处填写您的 Binance API Key
	secretKey       = ""                       // 在此处填写您的 Binance Secret Key
	proxyURL        = "http://127.0.0.1:10809" // 如果需要使用代理，请在此处填写代理地址
	timeInternal_1h = "1h"
	timeInternal_4h = "4h"
	klinesCount     = 200
	goroutineCount  = 80
)

var (
	volumeMap    = make(map[string]float64)
	MA25Map1h    = make(map[string]float64)
	mu_MA25Map1h sync.Mutex
)

func main() {
	// 创建 Binance 客户端（不需要 API 密钥即可获取市场数据）
	//client := binance.NewProxiedClient(apiKey, secretKey, proxyURL)
	client := binance.NewFuturesClient(apiKey, secretKey)
	proxy, _ := url.Parse(proxyURL)
	tr := &http.Transport{
		Proxy:           http.ProxyURL(proxy),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client.HTTPClient = &http.Client{
		Transport: tr,
	}

	exchangeInfo, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		log.Fatalf("获取交易所信息失败: %v", err)
	}

	// 只考虑 USDT 交易对
	var symbols []string
	for _, s := range exchangeInfo.Symbols {
		if s.QuoteAsset == "USDT" && s.Status == "TRADING" {
			symbols = append(symbols, s.Symbol)
		}
	}

	//获取所有币种的24小时交易额
	utils.Get24HVolume(client, volumeMap)

	var (
		results []CoinIndicator
		mu      sync.Mutex
		wg      sync.WaitGroup
	)
	sem := semaphore.NewWeighted(int64(goroutineCount))
	//api并发限流
	// step1: 先处理所有 1h
	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			if err := sem.Acquire(context.Background(), 1); err != nil {
				return
			}
			defer sem.Release(1)

			result_1h, ok := processSymbol(client, sym, timeInternal_1h)
			if ok {
				mu.Lock()
				results = append(results, result_1h)
				mu.Unlock()
			}
		}(symbol)
	}
	wg.Wait()

	// step2: 再处理所有 4h
	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			if err := sem.Acquire(context.Background(), 1); err != nil {
				return
			}
			defer sem.Release(1)

			result_4h, ok := processSymbol(client, sym, timeInternal_4h)
			if ok {
				mu.Lock()
				results = append(results, result_4h)
				mu.Unlock()
			}
		}(symbol)
	}
	wg.Wait()

	// 按照交易量 排序结果
	sort.Slice(results, func(i, j int) bool {
		return results[i].Volume24H > results[j].Volume24H
	})

	// 打印结果
	fmt.Println("")
	fmt.Println("")
	fmt.Println("		《1H———符合条件的币种列表》		")
	fmt.Println("Symbol         Volume       相交趋势       止损")
	fmt.Println("----------------------------------------------------------------------")
	for _, result := range results {
		if result.TimeInternal != "1h" {
			continue
		}
		var trend string
		if result.Histogram > 0 {
			trend = "水上金叉"
		} else {
			trend = getConvergingStatus(result.MACD, result.Signal)
		}
		fmt.Printf("%-14s %-12.0f %-10s %-12.2f\n",
			result.Symbol,
			result.Volume24H,
			trend,
			result.StopLossRate,
		)
	}
	fmt.Println("")
	fmt.Println("")
	fmt.Println("		《4H———符合条件的币种列表》		")
	fmt.Println("Symbol         Volume       相交趋势       止损")
	fmt.Println("----------------------------------------------------------------------")
	for _, result := range results {
		if result.TimeInternal != "4h" {
			continue
		}
		var trend string
		if result.Histogram > 0 {
			trend = "水上金叉"
		} else {
			trend = getConvergingStatus(result.MACD, result.Signal)
		}
		fmt.Printf("%-14s %-12.0f %-10s %-12.2f\n",
			result.Symbol,
			result.Volume24H,
			trend,
			result.StopLossRate,
		)
	}
}

// 获取两线靠近状态描述
func getConvergingStatus(macd, signal float64) string {
	if (macd > signal && macd-signal < 0.0005) || (signal > macd && signal-macd < 0.0005) {
		return "即将相交"
	} else if math.Abs(macd-signal)/math.Abs(signal) < 0.03 {
		return "正纠缠中"
	} else {
		return "正在靠近"
	}
}

func processSymbol(client *futures.Client, symbol string, timeInternal string) (CoinIndicator, bool) {
	//单个超时控制
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	klines, err := client.NewKlinesService().
		Symbol(symbol).
		Interval(timeInternal).
		Limit(klinesCount).
		Do(ctx)
	if err != nil || len(klines) < 35 {
		return CoinIndicator{}, false
	}

	closes := make([]float64, len(klines))
	for i, k := range klines {
		c, err := strconv.ParseFloat(k.Close, 64)
		if err != nil {
			return CoinIndicator{}, false
		}
		closes[i] = c
	}

	currentPrice := closes[len(closes)-1]
	ma25 := utils.CalculateMA(closes, 25)
	ma100 := utils.CalculateMA(closes, 100) //4小时的MA25
	ema144 := utils.CalculateEMA(closes, 144)
	ema169 := utils.CalculateEMA(closes, 169)
	macdLine, signalLine, histogram := utils.CalculateMACD(closes, 12, 26, 9)
	//先将MA25保存
	if timeInternal == "1h" {
		mu_MA25Map1h.Lock()
		MA25Map1h[symbol] = ma25
		mu_MA25Map1h.Unlock()
	}

	//过滤条件
	//只过滤出在EMA144和EMA169之上
	if currentPrice < ema144[len(ema144)-1] || currentPrice < ema169[len(ema169)-1] {
		return CoinIndicator{}, false
	}

	//只过滤在MA25之上, 4小时在MA25之下进行一次标记
	priceToMA25 := currentPrice - ma25
	priceToMA100 := currentPrice - ma100
	if priceToMA25 < 0 {
		return CoinIndicator{}, false
	}
	if timeInternal == "1h" && priceToMA100 < 0 {
		return CoinIndicator{}, false
	}
	//最新是红柱，或者金叉
	currentHistogram := histogram[len(histogram)-1]
	preHistogram := histogram[len(histogram)-2]
	if currentHistogram > 0 {
		if preHistogram > 0 {
			return CoinIndicator{}, false
		}
	}

	//计算最大止损
	var stopLossRate float64
	if timeInternal == "4h" {
		ma25_1h, ok := MA25Map1h[symbol]
		if ok && ma25_1h != 0 {
			stopLossRate = math.Abs(currentPrice-ma25_1h) / ma25_1h * 100
		}
	}

	if timeInternal == "1h" {
		stopLossRate = math.Abs(currentPrice-ma25) / ma25 * 100
	}

	deaAboveZero := signalLine[len(signalLine)-1] > 0
	currentDistance := math.Abs(macdLine[len(macdLine)-1] - signalLine[len(signalLine)-1])
	previousDistance := math.Abs(macdLine[len(macdLine)-2] - signalLine[len(signalLine)-2])
	gettingCloser := currentDistance < previousDistance
	macdRate := macdLine[len(macdLine)-1] - macdLine[len(macdLine)-2]
	signalRate := signalLine[len(signalLine)-1] - signalLine[len(signalLine)-2]
	crossingTrend := (macdLine[len(macdLine)-1] < signalLine[len(signalLine)-1] && macdRate > signalRate) ||
		(macdLine[len(macdLine)-1] > signalLine[len(signalLine)-1] && macdRate < signalRate)
	crossed := currentHistogram > 0 && preHistogram < 0
	entangled := currentDistance/math.Abs(signalLine[len(signalLine)-1]) < 0.05

	if deaAboveZero && (gettingCloser || entangled) && (crossingTrend || crossed) {
		return CoinIndicator{
			Symbol:       symbol,
			Price:        currentPrice,
			MA25:         ma25,
			MACD:         macdLine[len(macdLine)-1],
			Signal:       signalLine[len(signalLine)-1],
			Histogram:    histogram[len(histogram)-1],
			TimeInternal: timeInternal,
			Volume24H:    volumeMap[symbol],
			StopLossRate: stopLossRate,
		}, true
	}
	return CoinIndicator{}, false
}
