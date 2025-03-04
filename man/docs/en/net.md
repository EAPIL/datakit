
# Net
---

{{.AvailableArchs}}

---

Net collector is used to collect host network information, such as traffic information of each network interface. For Linux, system-wide TCP and UDP statistics will be collected.

## Preconditions {#requirements}

None

## Configuration {#config}

=== "Host Installation"

    Go to the `conf.d/{{.Catalog}}` directory under the DataKit installation directory, copy `{{.InputName}}.conf.sample` and name it `{{.InputName}}.conf`. Examples are as follows:
    
    ```toml
    {{ CodeBlock .InputSample 4 }}
    ```
    
    Once configured, [restart DataKit](datakit-service-how-to.md#manage-service) 即可。

=== "Kubernetes"

    Support modifying configuration parameters as environment variables:
    
    | Environment Variable Name                                | Corresponding Configuration Parameter Item            | Parameter Example                                                     |
    | :---                                      | ---                         | ---                                                          |
    | `ENV_INPUT_NET_IGNORE_PROTOCOL_STATS`     | `ignore_protocol_stats`     | `true`/`false`                                               |
    | `ENV_INPUT_NET_ENABLE_VIRTUAL_INTERFACES` | `enable_virtual_interfaces` | `true`/`false`                                               |
    | `ENV_INPUT_NET_TAGS`                      | `tags`                      | `tag1=value1,tag2=value2`; If there is a tag with the same name in the configuration file, it will be overwritten. |
    | `ENV_INPUT_NET_INTERVAL`                  | `interval`                  | `10s`                                                        |
    | `ENV_INPUT_NET_INTERFACES`                | `interfaces`                | `'''eth[\w-]+''', '''lo'''` 以英文逗号隔开                   |

## Measurements {#measurements}

For all of the following data collections, a global tag named `host` is appended by default (the tag value is the host name of the DataKit), or other tags can be specified in the configuration by `[inputs.net.tags]`:

``` toml
 [inputs.net.tags]
  # some_tag = "some_value"
  # more_tag = "some_other_value"
  # ...
```

{{ range $i, $m := .Measurements }}

### `{{$m.Name}}`

- tag

{{$m.TagsMarkdownTable}}

- metric list

{{$m.FieldsMarkdownTable}}

{{ end }}



## More Readings {#more-readings}

- [eBPF data collection](ebpf.md)
