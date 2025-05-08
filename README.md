# MACD 交易信号筛选程序

## 程序概述

本程序是一个基于 MACD 指标的加密货币交易信号筛选工具，通过 Binance 交易所 API 获取市场数据，筛选出符合特定 MACD 形态的交易对。主要目标是识别 DEA 在零轴上方、两线逐渐靠近并可能形成交叉信号的情况。

## 数据获取流程

- 使用 Binance API 客户端连接交易所（无需 API 密钥，仅获取公开市场数据）
- 获取所有交易对信息，并筛选出 USDT 计价的交易对
- 限制初始测试数量为前 100 个交易对
- 针对每个交易对获取 4 小时周期的 K 线数据（100 根 K 线）

## 指标计算

程序计算以下技术指标：

- MA25：25 周期简单移动平均线
- MACD：使用标准参数(12, 26, 9)
  - DIF（MACD 线）：12 周期 EMA - 26 周期 EMA
  - DEA（信号线）：9 周期 DIF 的 EMA
  - 柱状图（Histogram）：DIF - DEA

## 筛选条件

程序使用以下条件筛选交易对：

- DEA 在 0 值上方：`signalLine > 0`
  - 确保处于总体多头市场环境
- DIF 和 DEA 两线逐渐靠近或纠缠：
  - 计算当前周期两线距离：`currentDistance = |macdLine - signalLine|`
  - 计算前一周期两线距离：`previousDistance = |之前的 MACD 值 - 之前的信号值|`
  - 判断是否靠近：`currentDistance < previousDistance`
  - 判断是否纠缠：当两线距离小于信号线绝对值的 5% 视为纠缠
- 即将或正在形成交叉趋势：
  - 计算 DIF 和 DEA 的变化率
  - 判断是否有交叉趋势：
    - DIF 低于 DEA 但上升速度更快，或
    - DIF 高于 DEA 但下降速度更快

## 输出排序

结果按照价格与 MA25 的距离百分比排序，优先展示价格贴近 MA25 的交易对

显示每个交易对的详细信息：

- 交易对符号
- 当前价格
- DEA 位置（数值）
- 两线距离（百分比）
- 相交趋势描述（即将相交/纠缠中/正在靠近）
- 柱状图值

## 辅助函数

- `calculateMA`：计算简单移动平均线
- `calculateEMA`：计算指数移动平均线
- `calculateMACD`：计算 MACD 相关指标
- `getMACDValue/getSignalValue/getHistogramValue`：获取历史 MACD 值
- `getConvergingStatus`：判断两线靠近状态
- `areLinesConverging`：判断两线是否持续靠近

## 应用场景

该程序适用于：

- 寻找 MACD 金叉信号前的介入时机
- 在多头市场中寻找可能的上涨机会
- 对大量交易对进行快速筛选，减少人工分析工作量

## 代码结构

- `main.go`：主程序，包括数据获取、指标计算、筛选条件、输出排序等逻辑
- `utils` 目录：包含各种辅助函数，如计算 MA、EMA、MACD 等
- `.idea/.gitignore`：IDEA 的 gitignore 文件，忽略了一些默认的文件

## 使用方法

1.  填写 Binance API Key 和 Secret Key
2.  如需要使用代理，填写代理地址
3.  运行 `main.go` 文件

## 注意事项

- 本程序仅供学习和研究使用，不保证实际交易效果
- 请勿将 API Key 和 Secret Key 泄露给他人
- 请勿滥用 Binance API，以免被封禁
- 请遵守 Binance 的使用规定
- 请遵守当地的法律法规

## 参考资料

- Binance API 文档
- MACD 指标
