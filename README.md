# dingding-sso 项目功能
一个单独的服务，用来做钉钉扫码登录。 
员工用钉钉扫码后，系统获取员工信息，可以做内部系统的登录，员工离职账号自动失效。 
使用go语言编写, 零依赖。

## 背景 2021-07-01

公司内人员流动频繁，若干个项目组自建的后台系统身份认证管理不统一，带来一定安全隐患。近期发生某项目组后台被xss注入攻击，导致管理员session被劫持，后台被外人登录的严重安全事件。 
所以开发了这个服务，一要大家接入方便，二要融入一定安全保护措施。

## 部署方便

* 使用go语言开发，零依赖，代码下载之后，修改配置文件，然后`go build main.go; nohup ./main > /dev/null 2>&1 &` 即可运行服务
* 参考[钉钉接入文档](https://developers.dingtalk.com/document/app/scan-qr-code-to-login-3rdapp)后直接使用内置http包发起调用钉钉接口，不用下载钉钉的SDK之类的
* ticket作为key和用户信息作为value保存在内存变量sync.Map中，不用配置redis、数据库之类的
* 对业务方来说，接入方便，登录页面在js里调用一下`window.open(本服务扫码地址)`就可以集成钉钉扫码功能，业务方无需知晓钉钉的app id、app secret等参数

## 安全措施

* ticket生成规则，`sha256(毫秒 + 自增id + 用户UA + 用户ip + 过期秒数)` 长度86字节
* ticket哈希值由扫码时候的`ip + agent`生成，即使ticket被盗，认证也通不过
* 预留了二次认证方式

## 系统流程

```
前端流程
+--------[ 浏览器 ]--------+
|                          |
|  登录页面                |
|  window.open("扫码页面") | ----+
|                          |     |
+--------------------------+     |
                                 |     +--------[ 本服务 ]--------+
                                 |     |                          |
                                 +---> |  扫码页面                |
                                       |  1. 用户ip               |
                                       |  2. 用户userAgent        |
                                       |  3. 生成ticket           |
                                       |  4. 跳转钉钉官方的扫码页 |
                                       |                          |
                                       +--------------------------+
                                                |
                                                |
                                                |
                                                +---> [ 用户扫码 ] ---+
                                                                      |     +--------[ 本服务 ]--------+
                                                                      |     |                          |
                                                                      +---> |  回调页面                |
                                                                            |  1. 调取员工信息         |
                                                                            |  2. 调取部门信息         |
                                                                            |  3. 缓存到sync.Map中     |
                                                              +------------+|  4. 输出js通知opener窗口 |
                                                              |             |  5. 关闭本窗口           |
+--------[ 浏览器 ]--------+                                  |             |                          |
|                          |                                  |             +--------------------------+
|  登录页面                | <--------------- ticket ---------+
|  1. 登录表单提交         |                                                
|  2. ticket提交给服务端   |                                                
|                          |                                                
+--------------------------+


服务端流程
+--------[ 服务端 ]--------+
|                          |
|  接收表单                |
|  1. 调用fetch接口        | ----+
|                          |     |
|                          |     |
|                          |     |     +--------[ 本服务 ]--------+
|                          |     |     |                          |
|                          |     +---> |  fetch接口               |
|                          |           |  1. 检查ticket           |
|                          |           |  2. 检查过期与否         |
|                          |     +---+ |  3. 返回用户信息         |
|                          |     |     |                          |
|  2. 正常登录流程         | <---+     |                          |
+--------------------------+           +--------------------------+

```

## 浏览器代码
```
var domain = '配置文件中的domain';
var scanUrl = '配置文件中的scan_url';
var ttl = '3600'; // ticket过期时间, 必须小于配置文件中的ticket_max_ttl
window.open(domain + scanUrl + '?auto=1&ttl=' + ttl, 'dingdingScan', 'height=580, width=608, top=0, left=0, toolbar=no, menubar=no, scrollbars=no, resizable=no, location=no, status=no')
```

## 钉钉后台开启的权限
```
钉钉文档 https://developers.dingtalk.com/document/app/scan-qr-code-to-login-3rdapp
建立一个h5 app
权限要求如下
 - 个人手机号信息                        已开通
 - 通讯录个人信息读权限                   已开通
 - 企业员工手机号信息                     已开通
 - 通讯录部门信息读权限                   已开通
 - 成员信息读权限                        已开通
 - 企业外部联系人读取权限                  已开通
 - 调用企业API基础权限                    已开通
 - 调用OpenApp专有API时需要具备的权限      已开通
```