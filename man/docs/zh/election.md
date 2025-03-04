{{.CSS}}
# DataKit 选举
---

:fontawesome-brands-linux: :fontawesome-brands-windows: :fontawesome-brands-apple: :material-kubernetes: :material-docker:

---

当集群中只有一个被采集对象（如 Kubernetes），但是在批量部署情况下，多个 DataKit 的配置完全相同，都开启了对该中心对象的采集，为了避免重复采集，我们可以开启 DataKit 的选举功能。

## 选举配置 {#config}

=== "datakit.conf"

    编辑 `conf.d/datakit.conf`，选举有关的配置如下：
    
    ```toml
    [election]
      # 开启选举
      enable = false

      # 设置选举的命名空间(默认 default)
      namespace = "default"
    
      # 允许在数据上追加选举空间的 tag
      enable_namespace_tag = false
    
      ## election.tags: 选举相关全局标签
      [election.tags]
        #  project = "my-project"
        #  cluster = "my-cluster"
    ```
    
    如果要对多个 DataKit 区分选举，比如这 10 DataKit 和 另外 8 DataKit 分开选举，互相不干扰，可以配置 DataKit 命名空间。同一个命名空间下的 DataKit 参与同一起选举。
    
    开启选举后，如果同时开启 `enable_election_tag = true`（[:octicons-tag-24: Version-1.4.7](changelog.md#cl-1.4.7)），则在选举类采集的数据上，自动加上 tag: `election_namespace = <your-namespace-name>`。

    `conf.d/datakit.conf` 中开启选举后，在需要参加选举的采集器中配置 `election = true`（目前支持选举的采集器的配置文件中都带有 `election` 项）

    注意：支持选举但配置为 `election = false` 的采集器不参与选举，其采集行为、tag 设置均不受选举影响；如果 datakit.conf 关闭选举，但采集器开启选举，其采集行为、tag 设置均与关闭选举相同。

=== "Kubernetes"

    参见[这里](datakit-daemonset-deploy.md#env-elect)

## 选举原理 {#how}

以 MySQL 为例，在同一个集群（如 k8s cluster）中，假定有 10 DataKit、2 个 MySQL 实例，且 DataKit 都开启了选举（Daemonset 模式下，每个 DataKit 的配置都是一样的）以及 MySQL 采集器：

- 一旦某个 DataKit 被选举上，那么所有 MySQL （其它选举类的采集也一样）的数据采集，都将由该 DataKit 来采集，不管被采集对象是一个还是多个，赢者通吃。其它未选上的 DataKit 出于待命状态。
- 观测云中心会判断当前选上的 DataKit 是否正常，如果异常，则强行踢掉该 DataKit，其它待命状态的 DataKit 将替代它
- 未开启选举的 DataKit（可能它不在当前集群中），如果也配置了 MySQL 采集，不受选举约束，它仍然会去采集 MySQL 的数据
- 选举的范围是 `工作空间+命名空间` 级别的，单个 `工作空间+命名空间` 中，一次最多只能有一个 DataKit 被选上
    - 关于工作空间，在 datakit.conf 中，通过 DataWay 地址串中的 `token` URL 参数来表示，每个工作空间，都有其对应 token
    - 关于选举的命名空间，在 datakit.conf 中，通过 `namespace` 配置项来表示。一个工作空间可以配置多个命名空间

## 选举类采集器的全局 tag 设置 {#global-tags}

=== "datakit.conf"

    在 `conf.d/datakit.conf` 开启选举的条件下，开启了选举的采集器采集到的数据，均会尝试追加 datakit.conf 中的 global-env-tag：
    
    ```toml
    [global_election_tags]
      # project = "my-project"
      # cluster = "my-cluster"
    ```

    如果原始数据上就带有了 `global_election_tags` 中的相应 tag，则以原始数据中带有的 tag 为准，此处不会覆盖。

    如果没有开启选举，则选举采集器采集到的数据中，均会带上 datakit.conf 中配置的 `global_host_tags`（跟非选举类采集器一样）：[:octicons-tag-24: Version-1.4.8](changelog.md#cl-1.4.8) ·


    ```toml
    [global_host_tags]
      ip         = "__datakit_ip"
      host       = "__datakit_hostname"
    ```

=== "Kubernetes"

    Kubernetes 中选举的配置参见[这里](datakit-daemonset-deploy.md#env-elect)，全局 tag 的设置参见[这里](datakit-daemonset-deploy.md#env-common)。

## 支持选举的采集列表 {#inputs}

目前支持选举的采集器列表如下：

- [Apache](apache.md)
- [ElasticSearch](elasticsearch.md)
- [Gitlab](gitlab.md)
- [InfluxDB](influxdb.md)
- [Container](container.md)
- [MongoDB](mongodb.md)
- [MySQL](mysql.md)
- [NSQ](nsq.md)
- [Nginx](nginx.md)
- [PostgreSQL](postgresql.md)
- [Prom](prom.md)
- [RabbitMQ](rabbitmq.md)
- [Redis](redis.md)
- [Solr](solr.md)
- [TDengine](tdengine.md)

## FAQ {#faq}

### `host` 字段问题 {#host}

对于由参与选举的采集器采集的对象，比如 MySQL，由于采集其数据的 DataKit 可能会变迁（发生了选举轮换），故默认情况下，这类采集器采集的数据不会带上 `host` 这个 tag，以避免时间线增长。我们建议在 MySQL 采集器配置上，增加额外的 `tags` 字段：

```toml
[inputs.{{.InputName}}.tags]
  host = "real-mysql-instance-name"
```

这样，当 DataKit 发生选举轮换时，会继续沿用 tags 中配置的 `host` 字段。
