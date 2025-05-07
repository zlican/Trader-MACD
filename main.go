package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
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
	Symbol       string
	Price        float64
	MA25         float64
	MACD         float64
	Signal       float64
	Histogram    float64
	PriceToMA    float64
	TimeInternal string
}

var (
	apiKey          = ""                       // 在此处填写您的 Binance API Key
	secretKey       = ""                       // 在此处填写您的 Binance Secret Key
	proxyURL        = "http://127.0.0.1:10809" // 如果需要使用代理，请在此处填写代理地址
	timeInternal_1h = "1h"
	timeInternal_4h = "4h"
	klinesCount     = 200
	goroutineCount  = 50
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

	var (
		results []CoinIndicator
		mu      sync.Mutex
		wg      sync.WaitGroup
	)
	sem := semaphore.NewWeighted(int64(goroutineCount))
	//api并发限流
	for _, symbol := range symbols {
		//对每一个代币的获取开启协程
		wg.Add(2)
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

	// 按照与 MA25 的距离排序结果
	sort.Slice(results, func(i, j int) bool {
		return results[i].PriceToMA < results[j].PriceToMA
	})

	// 打印结果
	fmt.Println("")
	fmt.Println("")
	fmt.Println("		《1H———符合条件的币种列表》		")
	fmt.Println("Symbol         Price        DEA位置      两线距离     相交趋势     Histogram")
	fmt.Println("----------------------------------------------------------------------")
	for _, result := range results {
		if result.TimeInternal != "1h" {
			continue
		}
		distancePercent := math.Abs(result.MACD-result.Signal) / math.Abs(result.Signal) * 100
		fmt.Printf("%-14s %-12.8f %-12.8f %-10.2f %-10s %-12.8f\n",
			result.Symbol,
			result.Price,
			result.Signal,
			distancePercent,
			getConvergingStatus(result.MACD, result.Signal),
			result.Histogram,
		)
	}
	fmt.Println("")
	fmt.Println("")
	fmt.Println("		《4H———符合条件的币种列表》		")
	fmt.Println("Symbol         Price        DEA位置      两线距离     相交趋势     Histogram")
	fmt.Println("----------------------------------------------------------------------")
	for _, result := range results {
		if result.TimeInternal != "4h" {
			continue
		}
		distancePercent := math.Abs(result.MACD-result.Signal) / math.Abs(result.Signal) * 100
		fmt.Printf("%-14s %-12.8f %-12.8f %-10.2f %-10s %-12.8f\n",
			result.Symbol,
			result.Price,
			result.Signal,
			distancePercent,
			getConvergingStatus(result.MACD, result.Signal),
			result.Histogram,
		)
	}
}

// 计算简单移动平均线
func calculateMA(data []float64, period int) float64 {
	if len(data) < period || period <= 0 {
		return 0
	}

	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

// 计算指数移动平均线
func calculateEMA(data []float64, period int) []float64 {
	ema := make([]float64, len(data))
	multiplier := 2.0 / float64(period+1)
	ema[0] = data[0]
	for i := 1; i < len(data); i++ {
		ema[i] = (data[i]-ema[i-1])*multiplier + ema[i-1]
	}
	return ema
}

// 计算 MACD
func calculateMACD(closePrices []float64, fastPeriod, slowPeriod, signalPeriod int) (macdLine, signalLine, histogram []float64) {
	emaFast := calculateEMA(closePrices, fastPeriod)
	emaSlow := calculateEMA(closePrices, slowPeriod)
	macdLine = make([]float64, len(closePrices))
	for i := range closePrices {
		macdLine[i] = emaFast[i] - emaSlow[i]
	}
	signalLine = calculateEMA(macdLine, signalPeriod)
	histogram = make([]float64, len(closePrices))
	for i := range closePrices {
		histogram[i] = macdLine[i] - signalLine[i]
	}
	return
}

// 获取两线靠近状态描述
func getConvergingStatus(macd, signal float64) string {
	if (macd > signal && macd-signal < 0.0005) || (signal > macd && signal-macd < 0.0005) {
		return "即将相交"
	} else if math.Abs(macd-signal)/math.Abs(signal) < 0.03 {
		return "纠缠中"
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
	ma25 := calculateMA(closes, 25)
	ema144 := calculateEMA(closes, 144)
	ema169 := calculateEMA(closes, 169)
	macdLine, signalLine, histogram := calculateMACD(closes, 12, 26, 9)

	//过滤条件
	//只过滤出在EMA144和EMA169之上
	if currentPrice < ema144[len(ema144)-1] || currentPrice < ema169[len(ema169)-1] {
		return CoinIndicator{}, false
	}

	//只过滤在MA25之上
	priceToMA := currentPrice - ma25
	if priceToMA < 0 {
		return CoinIndicator{}, false
	}

	//只过滤出红色柱子
	currentHistogram := histogram[len(histogram)-1]
	if currentHistogram > 0 {
		return CoinIndicator{}, false
	}

	deaAboveZero := signalLine[len(signalLine)-1] > 0
	currentDistance := math.Abs(macdLine[len(macdLine)-1] - signalLine[len(signalLine)-1])
	previousDistance := math.Abs(macdLine[len(macdLine)-2] - signalLine[len(signalLine)-2])
	gettingCloser := currentDistance < previousDistance
	macdRate := macdLine[len(macdLine)-1] - macdLine[len(macdLine)-2]
	signalRate := signalLine[len(signalLine)-1] - signalLine[len(signalLine)-2]
	crossingTrend := (macdLine[len(macdLine)-1] < signalLine[len(signalLine)-1] && macdRate > signalRate) ||
		(macdLine[len(macdLine)-1] > signalLine[len(signalLine)-1] && macdRate < signalRate)
	entangled := currentDistance/math.Abs(signalLine[len(signalLine)-1]) < 0.05

	if deaAboveZero && (gettingCloser || entangled) && crossingTrend {
		return CoinIndicator{
			Symbol:       symbol,
			Price:        currentPrice,
			MA25:         ma25,
			MACD:         macdLine[len(macdLine)-1],
			Signal:       signalLine[len(signalLine)-1],
			Histogram:    histogram[len(histogram)-1],
			PriceToMA:    priceToMA,
			TimeInternal: timeInternal,
		}, true
	}
	return CoinIndicator{}, false
}
