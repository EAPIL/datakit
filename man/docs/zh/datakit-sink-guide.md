# DataKit Sink 使用
---

## DataKit Sinker {#intro}

本文将讲述什么是 DataKit 的 Sinker 模块(以下简称 Sinker 模块、Sinker)、以及如何使用 Sinker 模块。

## 什么是 Sinker {#what}

Sinker 是 DataKit 中数据存储定义模块。默认情况下，DataKit 采集到的数据是上报给[观测云](https://console.guance.com/){:target="_blank"}，但通过配置不同的 Sinker 配置，我们可以将数据发送给不同的自定义存储。

### 目前支持的 Sinker 实例 {#list}

- [InfluxDB](datakit-sink-influxdb.md)：目前支持将 DataKit 采集的时序数据（M）发送到本地的 InfluxDB 存储。
- [Logstash](datakit-sink-logstash.md)：目前支持将 DataKit 采集的日志数据（L）发送到本地 Logstash 服务。
- [M3DB](datakit-sink-m3db.md)：目前支持将 DataKit 采集的时序数据（M）发送到本地的 InfluxDB 存储（同 InfluxDB）。
- [OpenTelemetry and Jaeger](datakit-sink-otel-jaeger.md)：OpenTelemetry(OTEL) 提供了多种 Export 将链路数据（T）发送到多个采集终端中，例如：Jaeger、otlp、zipkin、prometheus。
- [Dataway](datakit-sink-dataway.md)：目前支持将 DataKit 采集所有类型的数据发送到 Dataway 存储。

当让，同一定的开发，也能将现有 DataKit 采集到的各种其它数据发送到任何其它存储，参见[Sinker 开发文档](datakit-sink-dev.md)。

## Sinker 的配置 {#config}

只需要以下简单三步:

- 搭建后端存储，目前支持 [InfluxDB](datakit-sink-influxdb.md)、[Logstash](datakit-sink-logstash.md)、[M3DB](datakit-sink-m3db.md)、[OpenTelemetry and Jaeger](datakit-sink-otel-jaeger.md) 以及 [Dataway](datakit-sink-dataway.md)。

- 增加 Sinker 配置：在 `datakit.conf` 配置中增加 Sinker 实例的相关参数，也能在 DataKit 安装阶段即指定 Sinker 配置。具体参见各个已有 Sinker 的安装文档。

  - [InfluxDB 安装](datakit-sink-influxdb.md)
  - [Logstash 安装](datakit-sink-logstash.md)
  - [M3DB 安装](datakit-sink-m3db.md)
  - [OpenTelemetry and Jaeger 安装](datakit-sink-otel-jaeger.md)
  - [Dataway 安装](datakit-sink-dataway.md)

- 重启 DataKit

```shell
$ sudo datakit --restart
```

## 通用参数的说明 {#args}

无论哪种 Sinker 实例, 都必须支持以下参数:

- `target`: Sinker 实例目标, 即要写入的存储是什么，如 `influxdb`
- `categories`: 汇报数据的类型。如 `["M", "N", "K", "O", "CO", "L", "T", "R", "S"]`

`categories` 中各字符串对应的上报指标集如下:

| `categories` 字符串 | 对应数据类型 |
| ----                | ----         |
| `M`                 | Metric       |
| `N`                 | Network      |
| `K`                 | KeyEvent     |
| `O`                 | Object       |
| `CO`                | CustomObject |
| `L`                 | Logging      |
| `T`                 | Tracing      |
| `R`                 | RUM          |
| `S`                 | Security     |

> 注：对于未指定 Sinker 的 categories，默认仍然发送给观测云。

## 扩展阅读 {#more-readings}

- [Sinker 之 InfluxDB](datakit-sink-influxdb.md)
- [Sinker 之 Logstash](datakit-sink-logstash.md)
- [Sinker 之 M3DB](datakit-sink-m3db.md)
- [Sinker 之 OpenTelemetry and Jaeger](datakit-sink-otel-jaeger.md)
- [Sinker 之 Dataway](datakit-sink-dataway.md)
