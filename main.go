package main

// Project: dingding_sso
// Summary: 企业内部使用的钉钉扫码登录，是员工则返回用户信息，不是员工则提示非内部员工不允许登录。支持钉钉通讯录设置外部联系人的方式让外部合作方登录
// Author: JerryPan
// Date: 2021年9月7日 16:30:42
// Version: 0.3

// 钉钉文档 https://developers.dingtalk.com/document/app/scan-qr-code-to-login-3rdapp
// app权限要求
// - 个人手机号信息                        已开通
// - 通讯录个人信息读权限                   已开通
// - 企业员工手机号信息                     已开通
// - 通讯录部门信息读权限                   已开通
// - 成员信息读权限                        已开通
// - 根据手机号姓名获取成员信息的接口访问权限  已开通
// - 企业外部联系人读取权限                  已开通
// - 调用企业API基础权限                    已开通
// - 调用OpenApp专有API时需要具备的权限      已开通

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var logerFileName string
var logerFd *os.File
var loger *log.Logger

func init() {
	fileName := "./logs/" + time.Now().Format("2006-01") + "_log" + ".txt"
	fd, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0766)
	if err != nil {
		panic(err)
	}
	loger = log.New(fd, "", log.LstdFlags|log.Lshortfile)
	logerFd = fd
	logerFileName = fileName
}

var MemMap sync.Map
var MemMapTTL sync.Map
var MemTrustIpMap sync.Map
var MemForbiddenMap sync.Map

type TrustIpStruct struct {
	TotalLoginCount int64 `json:"total_login_count"` // 总共登录次数
	Expired         int64 `json:"expired"`           // 过期时间戳 到点会自动删除
}

type ForbiddenStruct struct {
	SsoName        string  `json:"sso_name"`         // 用户名
	SsoContactType float64 `json:"sso_contact_type"` // 0 内部联系人     1 外部联系人
	SsoMobile      string  `json:"sso_mobile"`       // 手机号
	Expired        int64   `json:"expired"`          // 过期时间戳 到点会自动删除
}

type SsoUserInfoStruct struct {
	SsoName             string              `json:"sso_name"`               // 用户名
	SsoContactType      float64             `json:"sso_contact_type"`       // 0 内部联系人     1 外部联系人
	SsoMobile           string              `json:"sso_mobile"`             // 手机号
	SsoUserDeptInfo     []SsoUserDeptStruct `json:"sso_user_dept_info"`     // 用户部门信息
	SsoAvatar           string              `json:"sso_avatar"`             // 头像链接
	SsoJobTitle         string              `json:"sso_job_title"`          // 工作岗位名称
	SsoStateCode        string              `json:"sso_state_code"`         // 国家编号
	SsoCompanyName      string              `json:"sso_company_name"`       // 公司名字  外部联系人才有
	SsoEmail            string              `json:"sso_email"`              // 邮箱  外部联系人才有
	SsoFollowerUserId   string              `json:"sso_follower_user_id"`   // 负责人userId  外部联系人才有
	SsoFollowerUser     *SsoUserInfoStruct  `json:"sso_follower_user"`      // 负责人结构体  外部联系人才有
	SsoAddress          string              `json:"sso_address"`            // 联系地址  外部联系人才有
	SsoRemark           string              `json:"sso_remark"`             // 备注  外部联系人才有
	SsoDingdingUnionId  string              `json:"sso_dingding_union_id"`  // 钉钉 公司内 分配的用户id
	SsoDingdingUserId   string              `json:"sso_dingding_user_id"`   // 钉钉 分配的用户id
	SsoDingdingOpenId   string              `json:"sso_dingding_open_id"`   // 钉钉 分配的open id
	SsoDingdingNickName string              `json:"sso_dingding_nick_name"` // 钉钉 设置的用户昵称
	SsoTicket           string              `json:"sso_ticket"`             // 扫码后业务方请求我的ticket
	DingdingRaw         DingdingRawStruct   `json:"dingding_raw"`           // 调用钉钉api取到的原始数据
}

type SsoUserDeptStruct struct {
	SsoDeptId      string `json:"sso_dept_id"`       // 所在部门id
	SsoDeptName    string `json:"sso_dept_name"`     // 所在部门名称
	SsoIsDeptOwner string `json:"sso_is_dept_owner"` // 是所在部门名称管理员
}

type DingdingRawStruct struct {
	UserInfo            string   `json:"user_info"`             // 钉钉返回的 /topapi/getuserinfo_bycode
	UserUnion           string   `json:"user_union"`            // 钉钉返回的 /topapi/getbyunionid
	User                string   `json:"user"`                  // 钉钉返回的 /topapi/user/get
	Departments         []string `json:"department_arr"`        // 钉钉返回的 /topapi/department/get
	ExternalContactInfo string   `json:"external_contact_info"` // 钉钉返回的 /topapi/extcontact/get
}

func clearExpiredTicket() {
	time.Sleep(time.Second * 5)

	now := time.Now().Unix()
	//fmt.Println("start", now)
	MemMapTTL.Range(func(key, value interface{}) bool {
		//fmt.Println(key)
		//fmt.Println(value.(int64) - now)
		if now >= value.(int64) {
			MemMap.Delete(key)
			MemMapTTL.Delete(key)
		}
		return true
	})
	go clearExpiredTicket()
}

func clearExpiredIp() {
	time.Sleep(time.Second * 5)

	now := time.Now().Unix()
	MemTrustIpMap.Range(func(key, value interface{}) bool {
		if now >= value.(TrustIpStruct).Expired {
			MemTrustIpMap.Delete(key)
			loger.Println("Delete trust ip:", key.(string))
		}
		return true
	})
	go clearExpiredIp()
}

func clearForbiddenIp() {
	time.Sleep(time.Second * 1)

	now := time.Now().Unix()
	MemForbiddenMap.Range(func(key, value interface{}) bool {
		if now >= value.(ForbiddenStruct).Expired {
			MemForbiddenMap.Delete(key)
		}
		return true
	})
	go clearForbiddenIp()
}

func changeLogger() {
	time.Sleep(time.Second * 60)
	fileName := "./logs/" + time.Now().Format("2006-01") + "_log" + ".txt"
	if fileName != logerFileName {
		fd, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0766)
		if err == nil {
			loger.SetOutput(fd)
			logerFd.Close()
			logerFd = fd
			logerFileName = fileName
		}
	}
	go changeLogger()
}

func main() {
	ReadFile()              // 读取配置文件
	go clearExpiredTicket() // 定期清理过期的内存sso用户数据
	go clearExpiredIp()     // 定期清理过期的可信ip
	go clearForbiddenIp()   // 定期清理禁止的ip
	go changeLogger()       // 定期更换日志文件

	// 配置文件校验
	if _, ok := ConfigMap.Load("domain"); !ok {
		panic("config domain not found")
	}
	if _, ok := ConfigMap.Load("title"); !ok {
		panic("config title not found")
	}

	if _, ok := ConfigMap.Load("two_factor_authentication"); !ok {
		panic("config two_factor_authentication not found")
	}
	if _, ok := ConfigMap.Load("trust_ip_store_duration"); !ok {
		panic("config trust_ip_store_duration not found")
	}
	if temp, ok := ConfigMap.Load("ticket_hash_secret"); !ok {
		if len(temp.(string)) == 0 {
			panic("config ticket_hash_secret not valid")
		}
		panic("config ticket_hash_secret not found")
	}
	if _, ok := ConfigMap.Load("ticket_max_ttl"); !ok {
		panic("config ticket_max_ttl not found")
	}
	if temp, ok := ConfigMap.Load("dingding_agent_id"); !ok {
		if len(temp.(string)) == 0 {
			panic("config dingding_agent_id not valid")
		}
		panic("config dingding_agent_id not found")
	}
	if temp, ok := ConfigMap.Load("dingding_app_key"); !ok {
		if len(temp.(string)) == 0 {
			panic("config dingding_app_key not valid")
		}
		panic("config dingding_app_key not found")
	}
	if temp, ok := ConfigMap.Load("dingding_app_secret"); !ok {
		if len(temp.(string)) == 0 {
			panic("config dingding_app_secret not valid")
		}
		panic("config dingding_app_secret not found")
	}

	var scanUrl, scanSuccessUrl, ticketUrl, ttlUrl, versionUrl, managerUrl, port string
	if temp, ok := ConfigMap.Load("scan_url"); ok {
		scanUrl = temp.(string)
	}
	if temp, ok := ConfigMap.Load("scan_success_url"); ok {
		scanSuccessUrl = temp.(string)
	}
	if temp, ok := ConfigMap.Load("ticket_url"); ok {
		ticketUrl = temp.(string)
	}
	if temp, ok := ConfigMap.Load("ttl_url"); ok {
		ttlUrl = temp.(string)
	}
	if temp, ok := ConfigMap.Load("version_url"); ok {
		versionUrl = temp.(string)
	}
	if temp, ok := ConfigMap.Load("manager_url"); ok {
		managerUrl = temp.(string)
	}
	if temp, ok := ConfigMap.Load("port"); ok {
		port = temp.(string)
	}

	if len(scanUrl) == 0 || len(scanSuccessUrl) == 0 || len(ticketUrl) == 0 || len(ttlUrl) == 0 || len(versionUrl) == 0 || len(managerUrl) == 0 || len(port) == 0 {
		panic("config param not valid")
	}

	http.Handle(scanUrl, scanHandler())               // 钉钉扫码页面 window.open(dingdingUrl, 'dingdingScan', 'height=580, width=608, top=0, left=0, toolbar=no, menubar=no, scrollbars=no, resizable=no, location=no, status=no')
	http.Handle(scanSuccessUrl, scanSuccessHandler()) // 扫码后, 钉钉服务器跳转回来的地址
	http.Handle(ticketUrl, fetchByTicketHandler())    // 让业务方调用, 用ticket来获取刚才扫码的用户信息
	http.Handle(ttlUrl, ttlByTicketHandler())         // 内部测试用, 查看ticket的过期时间秒
	http.Handle(managerUrl, managerHandler())         // 管理后台, 用来显示有哪些可信ip, 有哪些禁止的用户, 通过删除按钮可以删除它们
	http.HandleFunc(versionUrl, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0.31"))
	})

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=7776000")
		decodedStrAsByteSlice, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAABAAAAAQEAYAAABPYyMiAAAABmJLR0T///////8JWPfcAAAACXBIWXMAAABIAAAASABGyWs+AAAAF0lEQVRIx2NgGAWjYBSMglEwCkbBSAcACBAAAeaR9cIAAAAASUVORK5CYII=")
		w.Write(decodedStrAsByteSlice)
	})

	loger.Println("dingding sso server start listen on ", port)
	err := http.ListenAndServe(port, nil) // 开始监听端口
	if err != nil {
		panic("can not listen the port " + port + ", program exit now!")
	}
}

func ttlByTicketHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/json; charset=utf-8")
		switch req.Method {
		case "POST":
			if err := req.ParseForm(); err != nil {
				EchoJson(w, "err:20", nil)
				fmt.Println(err.Error())
				return
			}
			ticket := req.Form.Get("sso_ticket")
			if ticket == "" {
				w.WriteHeader(http.StatusNotImplemented)
				EchoJson(w, "err:21", nil)
				return
			}

			userAgent := req.Form.Get("user_agent")
			userIp := req.Form.Get("client_ip")
			ok, _ := checkTicket(ticket, userAgent, userIp, "fetch")
			if !ok {
				w.WriteHeader(http.StatusGone)
				EchoJson(w, "err:28", nil)
				return
			}

			now := time.Now().Unix()
			if expire, ok := MemMapTTL.Load(ticket); ok {
				val := expire.(int64) - now
				w.Write([]byte(fmt.Sprintf("%d", val)))
				return
			}
			w.Write([]byte("ticket not found"))
			return
		default:
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
	}
}

func fetchByTicketHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/json; charset=utf-8")
		switch req.Method {
		case "POST":
			//gets := req.URL.Query()
			//if _, ok := gets["sso_ticket"]; !ok {
			//	w.WriteHeader(http.StatusNotImplemented)
			//	return
			//}
			//ticket := gets["sso_ticket"][0]
			if err := req.ParseForm(); err != nil {
				EchoJson(w, "err:20", nil)
				fmt.Println(err.Error())
				return
			}
			ticket := req.Form.Get("sso_ticket")
			renew := req.Form.Get("renew")
			if ticket == "" {
				w.WriteHeader(http.StatusNotImplemented)
				EchoJson(w, "err:21", nil)
				return
			}

			userAgent := req.Form.Get("user_agent")
			userIp := req.Form.Get("client_ip")
			ok, ttl := checkTicket(ticket, userAgent, userIp, "fetch")
			if !ok {
				w.WriteHeader(http.StatusGone)
				EchoJson(w, "err:28", nil)
				return
			}

			now := time.Now().Unix()
			if jsonByte, ok := MemMap.Load(ticket); ok {
				if expire, ok := MemMapTTL.Load(ticket); ok {
					if now >= expire.(int64) {
						MemMap.Delete(ticket)
						MemMapTTL.Delete(ticket)
						EchoJson(w, "err:22", nil)
						return
					}
					if renew == "1" {
						if allowTicketRenew, ok := ConfigMap.Load("allow_ticket_renew"); ok {
							if allowTicketRenew.(string) == "yes" {
								remoteIp := strings.Split(req.RemoteAddr, ":")[0]
								if isInnerIp(remoteIp) { // 内网发起才允许续期过期时间
									MemMapTTL.Store(ticket, now+int64(ttl))
								}
							}
						}
					}
					EchoJson(w, "0", jsonByte.([]byte)) // 无异常
					return
				}
			}
			EchoJson(w, "err:22", nil)
			return
		default:
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
	}
}

func scanSuccessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var isGet = false
		switch req.Method {
		case "GET":
			isGet = true
			fallthrough
		case "POST":
			gets := req.URL.Query()
			if _, ok := gets["code"]; !ok {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			code := gets["code"][0]
			if _, ok := gets["state"]; !ok {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			ticket := gets["state"][0]
			userAgent := req.Header.Get("User-Agent")
			userIp := GetIp(req)

			ok, ttl := checkTicket(ticket, userAgent, userIp, "scan")
			if !ok {
				w.WriteHeader(http.StatusGone)
				EchoJs(w, "err:23", nil)
				return
			}

			dingdingRawStruct := DingdingRawStruct{}

			timestamp := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
			dingdingAppSecretTemp, _ := ConfigMap.Load("dingding_app_secret")
			dingdingAppSecret := dingdingAppSecretTemp.(string)
			signature := GetDingdingSignature(timestamp, dingdingAppSecret)

			dingdingAppKeyTemp, _ := ConfigMap.Load("dingding_app_key")
			dingdingAppKey := dingdingAppKeyTemp.(string)
			postUrl := fmt.Sprintf("https://oapi.dingtalk.com%s?accessKey=%s&timestamp=%s&signature=%s", "/sns/getuserinfo_bycode", dingdingAppKey, timestamp, signature)
			respBody, respMap, err := FetchDingApi(postUrl, `{"tmp_auth_code":"`+code+`"}`, "POST")
			loger.Println(string(respBody))
			dingdingRawStruct.UserInfo = string(respBody)
			// {"errcode":0,"errmsg":"ok","user_info":{"nick":"潘**","unionid":"uT19di******HpS5hGk**QiEiE","dingId":"$:LWCP_v1:$**zuci4Nk**g==","openid":"b5B**fR04Xf**AiEiE","main_org_auth_high_level":true}}
			// {"errcode":0,"errmsg":"ok","user_info":{"nick":"潘潘😎","unionid":"2r08DW******3i**iEiE","dingId":"$:LWCP_v1:$1A**7AaNwShqJx**rRNO","openid":"TPM0**D**XwiEiE","main_org_auth_high_level":false}}
			if err != nil {
				w.WriteHeader(http.StatusForbidden)
				EchoJs(w, "err:1", respBody)
				loger.Println(err.Error())
				return
			}
			if _, isset := respMap["user_info"]; !isset {
				w.WriteHeader(http.StatusInternalServerError)
				EchoJs(w, "err:2", respBody)
				return
			}
			userInfo := respMap["user_info"].(map[string]interface{})
			if _, isset := userInfo["unionid"]; !isset {
				w.WriteHeader(http.StatusInternalServerError)
				EchoJs(w, "err:3", respBody)
				return
			}
			ssoDingdingUnionId := userInfo["unionid"].(string)

			var ssoDingdingOpenId string
			if _, isset := userInfo["openid"]; !isset {
				ssoDingdingOpenId = ""
			} else {
				ssoDingdingOpenId = userInfo["openid"].(string)
			}
			var ssoDingdingNickName string
			if _, isset := userInfo["nick"]; !isset {
				ssoDingdingNickName = ""
			} else {
				ssoDingdingNickName = userInfo["nick"].(string)
			}

			//MemMap.Store("accessToken", "7e139c**bec2f7e2") // 调试用

			var accessToken string
			if accessTokenLoaded, ok := MemMap.Load("accessToken"); ok {
				accessToken = accessTokenLoaded.(string)
			} else {
				respBody, accessToken, ok = GetDingdingAccessToken(dingdingAppKey, dingdingAppSecret)
				if !ok {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, accessToken, respBody)
					return
				}
				MemMap.Store("accessToken", accessToken)
			}

			errCount := 1
		retoken:
			postUrl = fmt.Sprintf("https://oapi.dingtalk.com/topapi/user/getbyunionid?access_token=%s", accessToken)
			respBody, respMap, err = FetchDingApi(postUrl, `{"unionid":"`+ssoDingdingUnionId+`"}`, "POST")
			loger.Println(string(respBody))
			dingdingRawStruct.UserUnion = string(respBody)
			// 内部员工 {"errcode":0,"errmsg":"ok","result":{"contact_type":0,"userid":"0138**711**71"},"request_id":"8e7a**vz**uwl"}
			// 外部联系人 {"errcode":0,"errmsg":"ok","result":{"contact_type":1,"userid":"0121281**19**912**8"},"request_id":"fmf**ma**loop0"}
			// 陌生人 {"errcode":60121,"errmsg":"找不到该用户","request_id":"xyv**o2**y4n4"}
			if err != nil {
				// {"errcode":88,"sub_code":"40014","sub_msg":"不合法的access_token","errmsg":"ding talk error[subcode=40014,submsg=不合法的access_token]","request_id":"h4**x7**xjj6"}
				if respMap != nil {
					if _, isset := respMap["sub_code"]; isset {
						if respMap["sub_code"].(string) == "40014" { // access token 过期   重新生成一个
							errCount++
							if errCount < 3 {
								var ok bool
								respBody, accessToken, ok = GetDingdingAccessToken(dingdingAppKey, dingdingAppSecret)
								if !ok {
									w.WriteHeader(http.StatusInternalServerError)
									EchoJs(w, accessToken, respBody)
									return
								}
								MemMap.Store("accessToken", accessToken)
								loger.Println("------------------ accessToken refresh:", accessToken)
								goto retoken
							}
						}
					}

					if _, isset := respMap["errcode"]; isset {
						if respMap["errcode"].(float64) == 60121 { // 找不到该用户
							w.WriteHeader(http.StatusInternalServerError)
							EchoJs(w, "err:3:1", respBody)
							loger.Println(err.Error())
							return
						}
					}
				}

				w.WriteHeader(http.StatusInternalServerError)
				EchoJs(w, "err:4", respBody)
				loger.Println(err.Error())
				return
			}
			if _, isset := respMap["result"]; !isset {
				w.WriteHeader(http.StatusInternalServerError)
				EchoJs(w, "err:5", respBody)
				return
			}
			result := respMap["result"].(map[string]interface{})
			if _, isset := result["contact_type"]; !isset {
				w.WriteHeader(http.StatusInternalServerError)
				EchoJs(w, "err:6", respBody)
				return
			}

			if _, isset := result["userid"]; !isset {
				w.WriteHeader(http.StatusInternalServerError)
				EchoJs(w, "err:7", respBody)
				return
			}
			ssoDingdingUserId := result["userid"].(string)

			var ssoContactType float64
			ssoContactType = result["contact_type"].(float64)

			var externalUser SsoUserInfoStruct
			var isExternalUser bool
		fetchNeibuUser:
			if ssoContactType == 0 { // 0 内部联系人     1 外部联系人
				postUrl = fmt.Sprintf("https://oapi.dingtalk.com/topapi/v2/user/get?access_token=%s", accessToken)
				respBody, respMap, err = FetchDingApi(postUrl, `{"userid":"`+ssoDingdingUserId+`"}`, "POST")
				// 内部员工调这个接口返回 {"errcode":0,"errmsg":"ok","result":{"active":true,"admin":true,"avatar":"","boss":false,"dept_id_list":[**008**187],"dept_order_list":[{"dept_id":**008**187,"order":**62921**72512}],"exclusive_account":false,"hide_mobile":false,"hired_date":1**506880**00,"job_number":"00021116","leader_in_dept":[{"dept_id":**00**4187,"leader":false}],"mobile":"150**66**01","name":"潘****","real_authed":true,"role_list":[{"group_name":"默认","id":57**22**0,"name":"子管理员"}],"senior":false,"state_code":"86","title":"架构师","union_emp_ext":{},"unionid":"uT1**iPn**HpS5h**QiE**E","userid":"01**110528**03**1"},"request_id":"4mo**qs**p3**h"}
				// 外部联系人调这个接口返回 {"errcode":60121,"errmsg":"找不到该用户","request_id":"wgd**pxca**z"}
				loger.Println(string(respBody))
				dingdingRawStruct.User = string(respBody)
				if err != nil {
					if _, isset := respMap["errcode"]; isset {
						if respMap["errcode"].(float64) == 60121 { // 找不到该用户
							w.WriteHeader(http.StatusInternalServerError)
							EchoJs(w, "err:9:1", respBody)
							loger.Println(err.Error())
							return
						}
					}

					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:9", respBody)
					loger.Println(err.Error())
					return
				}
				if _, isset := respMap["result"]; !isset {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:10", respBody)
					return
				}
				result = respMap["result"].(map[string]interface{})
				if _, isset := result["active"]; !isset {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:11", respBody)
					return
				}
				if result["active"].(bool) != true {
					w.WriteHeader(http.StatusForbidden)
					EchoJs(w, "err:12", nil)
					return
				}
				if _, isset := result["dept_id_list"]; !isset {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:13", respBody)
					return
				}
				var ssoStateCode string
				if _, isset := result["state_code"]; !isset {
					ssoStateCode = ""
				} else {
					ssoStateCode = result["state_code"].(string)
				}
				var ssoAvatar string
				if _, isset := result["avatar"]; !isset {
					ssoAvatar = ""
				} else {
					ssoAvatar = result["avatar"].(string)
				}
				var ssoJobTitle string
				if _, isset := result["title"]; !isset {
					ssoJobTitle = ""
				} else {
					ssoJobTitle = result["title"].(string)
				}
				var ssoName string
				if _, isset := result["name"]; !isset {
					ssoName = ""
				} else {
					ssoName = result["name"].(string)
				}
				var ssoMobile string
				if _, isset := result["mobile"]; !isset {
					ssoMobile = ""
				} else {
					ssoMobile = result["mobile"].(string)
				}
				deptIdList := result["dept_id_list"].([]interface{})
				if len(deptIdList) == 0 {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:14", respBody)
					return
				}
				// [427922115,447795618,487643026,427876169,427831197]
				var ssoUserDeptInfo []SsoUserDeptStruct
				var ssoDeptId string
				var ssoIsDeptOwner string
				for _, temp := range deptIdList {
					postUrl = fmt.Sprintf("https://oapi.dingtalk.com/topapi/v2/department/get?access_token=%s", accessToken)
					ssoDeptId = strconv.FormatInt(int64(temp.(float64)), 10)
					respBody, respMap, err = FetchDingApi(postUrl, `{"dept_id":"`+ssoDeptId+`"}`, "POST")
					// {"errcode":0,"errmsg":"ok","result":{"auto_add_user":true,"brief":"","create_dept_group":true,"dept_group_chat_id":"chat3b**550d137f8d5d7**15a56**","dept_id":**85****,"dept_manager_userid_list":["07*********61"],"dept_permits":[],"group_contain_sub_dept":false,"hide_dept":false,"name":"****部","order":**08**87,"org_dept_owner":"0**1711**50**","outer_dept":false,"outer_permit_depts":[],"outer_permit_users":[],"parent_id":**53**,"user_permits":[]},"request_id":"ij**bn**m"}
					loger.Println(string(respBody))
					dingdingRawStruct.Departments = append(dingdingRawStruct.Departments, string(respBody))
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						EchoJs(w, "err:15", respBody)
						loger.Println(err.Error())
						return
					}
					if _, isset := respMap["result"]; !isset {
						w.WriteHeader(http.StatusInternalServerError)
						EchoJs(w, "err:16", respBody)
						return
					}
					result = respMap["result"].(map[string]interface{})
					if _, isset := result["name"]; !isset {
						w.WriteHeader(http.StatusInternalServerError)
						EchoJs(w, "err:17", respBody)
						return
					}
					deptName := result["name"].(string)

					ssoIsDeptOwner = "0"
					if _, isset := result["dept_manager_userid_list"]; !isset {

					} else {
						for _, temp2 := range result["dept_manager_userid_list"].([]interface{}) {
							if temp2.(string) == ssoDingdingUserId {
								ssoIsDeptOwner = "1"
								break
							}
						}
					}
					ssoUserDeptInfo = append(ssoUserDeptInfo, SsoUserDeptStruct{
						SsoDeptId:      ssoDeptId,
						SsoDeptName:    deptName,
						SsoIsDeptOwner: ssoIsDeptOwner,
					})
					// fix 判断是否是部门管理员不用org_dept_owner字段 2021-12-07
					// if _, isset := result["org_dept_owner"]; isset { // 注意这个仅仅是群主userId, 不是部门管理员id
					// 	if ssoDingdingUserId == result["org_dept_owner"].(string) {
					// 		ssoIsDeptOwner = "1"
					// 	}
					// }
				}

				ssoUserInfo := SsoUserInfoStruct{
					SsoName:             ssoName,
					SsoContactType:      ssoContactType,
					SsoMobile:           ssoMobile,
					SsoUserDeptInfo:     ssoUserDeptInfo,
					SsoAvatar:           ssoAvatar,
					SsoJobTitle:         ssoJobTitle,
					SsoStateCode:        ssoStateCode,
					SsoDingdingUnionId:  ssoDingdingUnionId,
					SsoDingdingUserId:   ssoDingdingUserId,
					SsoDingdingOpenId:   ssoDingdingOpenId,
					SsoDingdingNickName: ssoDingdingNickName,
					DingdingRaw:         dingdingRawStruct,
				}

				if isExternalUser == false { // 内部员工
					if doTwoFactorAuthenticationCheck(w, req, ssoUserInfo, isGet, userIp, userAgent) == "exit" {
						return
					}
					successReturn(w, isExternalUser, ssoUserInfo, ticket, ttl, userIp, userAgent)
					return
				} else { // 外部联系人
					externalUser.SsoFollowerUser = &ssoUserInfo // 设置外部联系人的内部follow员工
					successReturn(w, isExternalUser, externalUser, ticket, ttl, userIp, userAgent)
					return
				}

			} else if ssoContactType == 1 { // 外部联系人(管理员在钉钉后台通讯录设置的)
				postUrl = fmt.Sprintf("https://oapi.dingtalk.com/topapi/extcontact/get?access_token=%s", accessToken)
				respBody, respMap, err = FetchDingApi(postUrl, `{"user_id":"`+ssoDingdingUserId+`"}`, "POST")
				// {"errcode":0,"errmsg":"ok","result":{"address":"地址(非必填)","company_name":"公司名(非必填)","email":"邮箱(非必填)","follower_user_id":"013**11052**371","label_ids":[94**085188,94**5190],"mobile":"131**87**7","name":"潘潘","remark":"备注(非必填)","share_dept_ids":[**85**7],"share_user_ids":[],"state_code":"86","title":"职位名(非必填)","userid":"01**281**291**8"},"request_id":"p**hd**z**n"}
				loger.Println(string(respBody))
				dingdingRawStruct.ExternalContactInfo = string(respBody)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:26", respBody)
					loger.Println(err.Error())
					return
				}
				if _, isset := respMap["result"]; !isset {
					w.WriteHeader(http.StatusInternalServerError)
					EchoJs(w, "err:27", respBody)
					return
				}
				result = respMap["result"].(map[string]interface{})
				var ssoStateCode string
				if _, isset := result["state_code"]; !isset {
					ssoStateCode = ""
				} else {
					ssoStateCode = result["state_code"].(string)
				}

				var ssoJobTitle string
				if _, isset := result["title"]; !isset {
					ssoJobTitle = ""
				} else {
					ssoJobTitle = result["title"].(string)
				}
				var ssoName string
				if _, isset := result["name"]; !isset {
					ssoName = ""
				} else {
					ssoName = result["name"].(string)
				}
				var ssoMobile string
				if _, isset := result["mobile"]; !isset {
					ssoMobile = ""
				} else {
					ssoMobile = result["mobile"].(string)
				}
				var ssoCompanyName string
				if _, isset := result["company_name"]; !isset {
					ssoCompanyName = ""
				} else {
					ssoCompanyName = result["company_name"].(string)
				}
				var ssoEmail string
				if _, isset := result["email"]; !isset {
					ssoEmail = ""
				} else {
					ssoEmail = result["email"].(string)
				}
				var ssoAddress string
				if _, isset := result["address"]; !isset {
					ssoAddress = ""
				} else {
					ssoAddress = result["address"].(string)
				}
				var ssoRemark string
				if _, isset := result["remark"]; !isset {
					ssoRemark = ""
				} else {
					ssoRemark = result["remark"].(string)
				}
				var ssoFollowerUserId string
				if _, isset := result["follower_user_id"]; !isset {
					ssoFollowerUserId = ""
				} else {
					ssoFollowerUserId = result["follower_user_id"].(string)
				}

				isExternalUser = true
				externalUser = SsoUserInfoStruct{
					SsoName:             ssoName,
					SsoContactType:      ssoContactType,
					SsoMobile:           ssoMobile,
					SsoCompanyName:      ssoCompanyName,
					SsoEmail:            ssoEmail,
					SsoAddress:          ssoAddress,
					SsoRemark:           ssoRemark,
					SsoFollowerUserId:   ssoFollowerUserId,
					SsoJobTitle:         ssoJobTitle,
					SsoStateCode:        ssoStateCode,
					SsoDingdingUnionId:  ssoDingdingUnionId,
					SsoDingdingUserId:   ssoDingdingUserId,
					SsoDingdingOpenId:   ssoDingdingOpenId,
					SsoDingdingNickName: ssoDingdingNickName,
					DingdingRaw:         dingdingRawStruct,
				}

				ssoDingdingUserId = ssoFollowerUserId
				ssoDingdingOpenId = ""
				ssoDingdingUnionId = ""
				ssoDingdingNickName = ""
				ssoContactType = 0
				dingdingRawStruct = DingdingRawStruct{}
				goto fetchNeibuUser
			}
			return
		default:
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
	}
}

func scanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var isGet = false
		switch req.Method {
		case "GET":
			isGet = true
			fallthrough
		case "POST":
			gets := req.URL.Query()
			var autoRedirect string
			if _, ok := gets["auto"]; !ok {
				autoRedirect = "0"
			} else {
				autoRedirect = gets["auto"][0]
			}
			var ttlIntt int
			if _, ok := gets["ttl"]; !ok {
				ttlIntt = 30
			} else {
				ttlInt, err := strconv.Atoi(gets["ttl"][0])
				if err != nil {
					ttlIntt = 30
				} else {
					ticketMaxTTL, _ := ConfigMap.Load("ticket_max_ttl")
					ticketMaxTTLInt, err2 := strconv.Atoi(ticketMaxTTL.(string))
					if err2 != nil {
						ttlIntt = 30
					} else {
						if ttlInt <= 0 || ttlInt > ticketMaxTTLInt {
							ttlIntt = 30
						} else {
							ttlIntt = ttlInt
						}
					}
				}
			}

			userAgent := req.Header.Get("User-Agent")
			userIp := GetIp(req)

			if _, ok := MemForbiddenMap.Load(userIp); ok {
				w.WriteHeader(http.StatusForbidden)
				EchoJs(w, "err:31", nil)
				return
			}

			domain, _ := ConfigMap.Load("domain")
			title, _ := ConfigMap.Load("title")
			scanSuccessUrl, _ := ConfigMap.Load("scan_success_url")

			if _, ok := gets["dev"]; ok { // POST and mock钉钉返回
				if strings.Split(req.RemoteAddr, ":")[0] == "127.0.0.1" {
					//time.Sleep(time.Second * 1)
					ok, ttl := checkTicket(gets["dev"][0], userAgent, userIp, "scan")
					if !ok {
						w.WriteHeader(http.StatusGone)
						EchoJs(w, "err:23", nil)
						return
					}
					ssoUserInfo := SsoUserInfoStruct{SsoName: "潘dev", SsoDingdingNickName: "潘Nick", SsoDingdingOpenId: "xxxxx"}
					if _, ok := MemForbiddenMap.Load(ssoUserInfo.SsoName); ok {
						w.WriteHeader(http.StatusForbidden)
						EchoJs(w, "err:33", nil)
						return
					}
					if doTwoFactorAuthenticationCheck(w, req, ssoUserInfo, isGet, userIp, userAgent) == "exit" {
						return
					}
					successReturn(w, false, ssoUserInfo, gets["dev"][0], ttl, userIp, userAgent)
					return
				}
				w.Write([]byte("Hi There, Please Contact Me At WeChat: JryPan87")) // 不是本机, 尝试dev参数, 是道友
				return
			}

			ticket := generateTicket(userAgent, userIp, ttlIntt)
			dingdingAppKeyTemp, _ := ConfigMap.Load("dingding_app_key")
			dingdingAppKey := dingdingAppKeyTemp.(string)
			dingdingUrl := `https://oapi.dingtalk.com/connect/qrconnect?appid=` + dingdingAppKey + `&response_type=code&scope=snsapi_login&state=` + ticket + `&redirect_uri=` + url.QueryEscape(domain.(string)+scanSuccessUrl.(string))
			if autoRedirect == "1" {
				http.Redirect(w, req, dingdingUrl, http.StatusFound)
				return
			}

			if strings.Split(req.RemoteAddr, ":")[0] != "127.0.0.1" {
				http.Redirect(w, req, dingdingUrl, http.StatusFound)
				return
			}

			ticketUrl, _ := ConfigMap.Load("ticket_url")
			ttlUrl, _ := ConfigMap.Load("ttl_url")

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<head>
<title>` + title.(string) + `</title>
<style>
body{font-size:28px;}
</style>
<script>
function dingdingOpen(dingdingUrl) {
	window.open(dingdingUrl, 'dingdingScan', 'height=580, width=608, top=0, left=0, toolbar=no, menubar=no, scrollbars=no, resizable=no, location=no, status=no')
}
function formSubmit(url, sso_ticket) {
    var f = document.createElement("form");
    f.method = 'post';
    f.action = url;
    f.target = '_blank';
    var newElement = document.createElement("input");
    newElement.setAttribute("name", "sso_ticket");
    newElement.setAttribute("type", "hidden");
    newElement.setAttribute("value", sso_ticket);
    f.appendChild(newElement);
    var newElement2 = document.createElement("input");
    newElement2.setAttribute("name", "renew");
    newElement2.setAttribute("type", "hidden");
    newElement2.setAttribute("value", "1");
    f.appendChild(newElement2);
    
    var newElement2 = document.createElement("input");
    newElement2.setAttribute("name", "user_agent");
    newElement2.setAttribute("type", "hidden");
    newElement2.setAttribute("value", "` + userAgent + `");
    f.appendChild(newElement2);
    
    var newElement3 = document.createElement("input");
    newElement3.setAttribute("name", "client_ip");
    newElement3.setAttribute("type", "hidden");
    newElement3.setAttribute("value", "` + userIp + `");
    f.appendChild(newElement3);

    document.body.appendChild(f);
    setTimeout(function(){f.submit();}, 200)
}
var handleMessage = function (event) {
    var origin = event.origin;
    if (origin == window.origin || origin == "` + domain.(string) + `") {
    	if (event.data.err == "0") {
  			document.body.innerHTML = "欢迎: " + event.data.detail.sso_dingding_nick_name + "! 扫码成功!<br>船票: <span href=\"javascript:;\" onclick=\"formSubmit('" + event.origin + "` + ticketUrl.(string) + `', '" + event.data.detail.sso_ticket + "');\" target=\"_blank\" style=\"cursor:pointer\">" + event.data.detail.sso_ticket + "</span><br>"
  			document.body.innerHTML += "<span href=\"javascript:;\" onclick=\"formSubmit('" + event.origin + "` + ttlUrl.(string) + `', '" + event.data.detail.sso_ticket + "');\" target=\"_blank\" style=\"cursor:pointer\">" + "show ttl" + "</span><br>"
  		} else {
  			document.body.innerHTML = "错误提示: " + event.data.err + "<br>" + JSON.stringify(event.data)
  		}
    }
};
if (typeof window.addEventListener != 'undefined') {
    window.addEventListener('message', handleMessage, false);
} else if (typeof window.attachEvent != 'undefined') {
    window.attachEvent('onmessage', handleMessage);
}
</script>
</head>
<body>
<a href="javascript:dingdingOpen('` + req.URL.Path + `?dev=` + ticket + `')">本地测试</a><br>
<a href="javascript:dingdingOpen('` + req.URL.Path + `?auto=1&ttl=300')">真的扫码</a>
<br>
ticket: ` + ticket + `
<br>
` + fmt.Sprintf("%d|%d|%s|%s|", time.Now().UnixNano()/1e6, GetCounterInt(), userAgent, userIp) + `

</body>
			`))
			return
		default:
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
	}
}

func managerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if strings.Split(req.RemoteAddr, ":")[0] != "127.0.0.1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		now := time.Now().Unix()
		switch req.Method {
		case "GET":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<title>管理后台</title>"))
			w.Write([]byte(`
<script>
function del(map_name, map_key) {
    var f = document.createElement("form");
    f.method = 'post';
     var newElement = document.createElement("input");
    newElement.setAttribute("name", "map_name");
    newElement.setAttribute("type", "hidden");
    newElement.setAttribute("value", map_name);
    f.appendChild(newElement);
    var newElement2 = document.createElement("input");
    newElement2.setAttribute("name", "map_key");
    newElement2.setAttribute("type", "hidden");
    newElement2.setAttribute("value", map_key);
    f.appendChild(newElement2);

    document.body.appendChild(f);
    setTimeout(function(){f.submit();}, 200)
}
</script>
`))
			twoFactorAuthentication, _ := ConfigMap.Load("two_factor_authentication")
			w.Write([]byte("双因素认证: " + twoFactorAuthentication.(string) + "<br><br>"))
			w.Write([]byte("信任列表<br>"))
			w.Write([]byte("<table style=\"border-collapse: collapse;border:3px solid #CCC\" cellpadding=\"15\" cellspacing=\"15\">"))
			w.Write([]byte("<tr>"))
			w.Write([]byte("<td>IP</td><td>成功登录次数</td><td>过期时间</td><td>剩余秒数</td><td>操作</td>"))
			w.Write([]byte("</tr>"))

			MemTrustIpMap.Range(func(key, value interface{}) bool {
				w.Write([]byte("<tr>"))
				w.Write([]byte(fmt.Sprintf("<td>%s</td><td>%d</td><td>%s</td><td>%d</td><td><a href=\"javascript:del('MemTrustIpMap','%s')\">删除</a></td>", key.(string), value.(TrustIpStruct).TotalLoginCount, time.Unix(value.(TrustIpStruct).Expired, 0).Format("2006-01-02 15:04:05"), value.(TrustIpStruct).Expired-now, key.(string))))
				w.Write([]byte("</tr>"))
				return true
			})
			w.Write([]byte("</table><br><br>"))

			w.Write([]byte("屏蔽列表<br>"))
			w.Write([]byte("<table style=\"border-collapse: collapse;border:3px solid #CCC\" cellpadding=\"15\" cellspacing=\"15\">"))
			w.Write([]byte("<tr>"))
			w.Write([]byte("<td>IP/OPEN_ID</td><td>用户名</td><td>内外部联系人</td><td>手机号</td><td>过期时间</td><td>剩余秒数</td><td>操作</td>"))
			w.Write([]byte("</tr>"))
			WaibuNeiBu := map[float64]string{0: "内部联系人", 1: "外部联系人"}
			MemForbiddenMap.Range(func(key, value interface{}) bool {
				w.Write([]byte("<tr>"))
				w.Write([]byte(fmt.Sprintf("<td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td><a href=\"javascript:del('MemForbiddenMap','%s')\">删除</a></td>", key.(string), value.(ForbiddenStruct).SsoName, WaibuNeiBu[value.(ForbiddenStruct).SsoContactType], value.(ForbiddenStruct).SsoMobile, time.Unix(value.(ForbiddenStruct).Expired, 0).Format("2006-01-02 15:04:05"), value.(ForbiddenStruct).Expired-now, key.(string))))
				w.Write([]byte("</tr>"))
				return true
			})
			w.Write([]byte("</table><br><br>"))

			w.Write([]byte("在线列表<br>"))
			w.Write([]byte("<table style=\"border-collapse: collapse;border:3px solid #CCC\" cellpadding=\"15\" cellspacing=\"15\">"))
			w.Write([]byte("<tr>"))
			w.Write([]byte("<td>ticket</td><td>操作</td><td>过期时间</td><td>剩余秒数</td><td>json</td>"))
			w.Write([]byte("</tr>"))
			MemMap.Range(func(key, value interface{}) bool {
				if key == "accessToken" {
					return true
				}
				w.Write([]byte("<tr>"))
				MemMapTTL.Load(key.(string))
				var expired int64 = 0
				if temp, ok := MemMapTTL.Load(key.(string)); ok {
					expired = temp.(int64)
				}
				w.Write([]byte(fmt.Sprintf("<td>%s</td><td><a href=\"javascript:del('MemMap','%s')\">删除</a></td><td>%s</td><td>%d</td><td id=\"%s\"><span>%s</span></td>", key.(string), key.(string), time.Unix(expired, 0).Format("2006-01-02 15:04:05"), expired-now, key.(string), value.([]byte))))
				w.Write([]byte("</tr>"))
				return true
			})
			w.Write([]byte("</table><br><br>"))

			return
		case "POST":
			if err := req.ParseForm(); err != nil {
				return
			}
			mapName := req.Form.Get("map_name")
			mapKey := req.Form.Get("map_key")
			if mapName == "" || mapKey == "" {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			if mapName == "MemTrustIpMap" {
				MemTrustIpMap.Delete(mapKey)
			}
			if mapName == "MemForbiddenMap" {
				MemForbiddenMap.Delete(mapKey)
			}
			if mapName == "MemMap" {
				MemMap.Delete(mapKey)
				MemMapTTL.Delete(mapKey)
			}
			http.Redirect(w, req, req.RequestURI, http.StatusFound)
			return
		default:
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
	}
}

func generateTicket(userAgent, userIp string, ttl int) string {
	now := time.Now().UnixNano() / 1e6
	key, _ := ConfigMap.Load("ticket_hash_secret")
	counter := GetCounterInt()
	checksum := hex.EncodeToString(Sha256(fmt.Sprintf("%d %d %s %s %d", now, counter, userAgent, userIp, 10000+ttl), key.(string)))
	//fmt.Println(fmt.Sprintf("%d %d %s %s %d", now, counter, userAgent, userIp, 10000+ttl))
	return fmt.Sprintf("%d%d%s%d", now, counter, checksum, 10000+ttl)
}

func checkTicket(tocheck, userAgent, userIp, where string) (bool, int) {
	key, _ := ConfigMap.Load("ticket_hash_secret")

	if len(tocheck) != 86 {
		return false, 0
	}
	if where == "scan" {
		old, err := strconv.Atoi(tocheck[:10])
		if err != nil {
			return false, 0
		}
		now2 := int(time.Now().Unix())
		if old+100 < now2 { // 给扫码的用户100秒扫码时间
			return false, 0
		}
	}
	// 16290920600601001cae543b91ca4278a80d8a3519550e2113ea609bb8e5604376cbacd95e845c8ae10300
	now := tocheck[:13]
	counter := tocheck[13:17]
	ttl := tocheck[17+64:]
	realCheckSum := hex.EncodeToString(Sha256(fmt.Sprintf("%s %s %s %s %s", now, counter, userAgent, userIp, ttl), key.(string)))
	//fmt.Println(fmt.Sprintf("%s %s %s %s %s", now, counter, userAgent, userIp, ttl))

	ttlInt, err := strconv.Atoi(ttl)
	if err != nil {
		return false, 0
	}

	return realCheckSum == tocheck[17:17+64], ttlInt - 10000
}

func Sha256(src, key string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(src))
	return m.Sum(nil)
}

func GetDingdingSignature(time, key string) string {
	return url.QueryEscape(base64.StdEncoding.EncodeToString(Sha256(time, key)))
}

func FetchDingApi(postUrl, postBody, method string) ([]byte, map[string]interface{}, error) {
	loger.Println("New Request,", postUrl, postBody)
	client := &http.Client{}
	request, err := http.NewRequest(method, postUrl, strings.NewReader(postBody))
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	//request.Header.Set("Cookie", "name=anny")
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	body, _ := ioutil.ReadAll(response.Body)
	respMap := make(map[string]interface{})
	err2 := json.Unmarshal(body, &respMap)
	if err2 != nil {
		return body, nil, errors.New(string(body))
	}
	if _, isset := respMap["errcode"]; !isset {
		return body, respMap, errors.New("no errcode in response")
	}

	if respMap["errcode"].(float64) != 0 {
		return body, respMap, errors.New("errcode not zero " + strconv.FormatInt(int64(respMap["errcode"].(float64)), 10))
	}

	return body, respMap, nil
}

func GetDingdingAccessToken(appKey, appSecret string) ([]byte, string, bool) {
	postUrl := fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s", appKey, appSecret)
	respBody, respMap, err := FetchDingApi(postUrl, `{}`, "GET")
	// {"errcode": 0, "access_token": "96fc7a7axxx", "errmsg": "ok", "expires_in": 7200}
	if err != nil {
		return respBody, "err:24", false
	}
	if _, isset := respMap["access_token"]; !isset {
		return respBody, "err:25", false
	}

	return nil, respMap["access_token"].(string), true
}

func successReturn(w http.ResponseWriter, isExternalUser bool, ssoUserInfo SsoUserInfoStruct, ticket string, ttl int, userIp string, userAgent string) {
	ssoUserInfo.SsoTicket = ticket

	ssoUserByte, err := json.Marshal(ssoUserInfo)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		EchoJs(w, "err:19", nil)
		return
	}

	now := time.Now().Unix()
	MemMap.Store(ticket, ssoUserByte)
	MemMapTTL.Store(ticket, now+int64(ttl))

	if accessTokenLoaded, ok := MemMap.Load("accessToken"); ok {
		accessToken := accessTokenLoaded.(string)

		if temp, ok := ConfigMap.Load("notify_user_id"); ok {
			notifyUserId := temp.(string)
			if notifyUserId != "" {
				title, _ := ConfigMap.Load("title")
				if isExternalUser == false { // 内部员工
					for _, _notifyUserId := range strings.Split(notifyUserId, ",") {
						SendDingdingText(title.(string), "  员工登录行为通知！姓名：**"+ssoUserInfo.SsoName+"**  登录ip："+userIp+"  手机号："+ssoUserInfo.SsoMobile+"  登录设备："+userAgent, _notifyUserId, accessToken)
					}
				} else { // 外部联系人
					if temp, ok := ConfigMap.Load("notify_dingding_id"); ok {
						notifyDingId := temp.(string)
						if notifyDingId != "" {
							SendDingdingText(title.(string), "  外部联系人登录行为通知，请注意！姓名：**"+ssoUserInfo.SsoName+"**  登录ip："+userIp+"  手机号："+ssoUserInfo.SsoMobile+"  有异常情况请立即[联系IT部门](dingtalk://dingtalkclient/action/sendmsg?dingtalk_id="+notifyDingId+")", ssoUserInfo.SsoFollowerUser.SsoDingdingUserId, accessToken)
							SendDingdingText(title.(string), "  外部联系人登录行为通知！姓名：**"+ssoUserInfo.SsoName+"**  登录ip："+userIp+"  手机号："+ssoUserInfo.SsoMobile+"  内部负责人："+ssoUserInfo.SsoFollowerUser.SsoName+"  登录设备："+userAgent, notifyUserId, accessToken)
						}
					}
				}
			}
		}
	}

	trustIpStoreDuration, _ := ConfigMap.Load("trust_ip_store_duration")
	trustIpStoreDurationInt, err := strconv.Atoi(trustIpStoreDuration.(string))
	if err == nil && trustIpStoreDurationInt > 0 {
		if trustIpStruct, ok := MemTrustIpMap.Load(userIp); !ok {
			MemTrustIpMap.Store(userIp, TrustIpStruct{TotalLoginCount: 1, Expired: now + int64(trustIpStoreDurationInt)})
		} else {
			MemTrustIpMap.Store(userIp, TrustIpStruct{TotalLoginCount: trustIpStruct.(TrustIpStruct).TotalLoginCount + 1, Expired: now + int64(trustIpStoreDurationInt)})
			trustIpStruct = nil
		}
		loger.Println("Add trust ip:", userIp)
	}

	loger.Println("Scan Success,", ssoUserInfo.SsoName, "登录成功, ip:", userIp, ", 登录设备:", userAgent)
	EchoJs(w, "0", ssoUserByte) // 无异常
}

func EchoJs(w http.ResponseWriter, err string, detail []byte) {
	w.Write([]byte(`<script>window.opener.postMessage(`))
	EchoJson(w, err, detail)
	w.Write([]byte(`, '*');window.close()</script>`))
	EchoJson(w, err, detail)
}

func EchoJson(w http.ResponseWriter, errId string, detail []byte) {
	var errMsg string
	if temp, ok := ConfigMap.Load(errId); !ok {
		errMsg = errId
	} else {
		errMsg = temp.(string)
	}

	if errId == "0" {
		w.Write([]byte(`{"err":"` + errMsg + `","detail":`))
		w.Write(detail)
		w.Write([]byte(`}`))
	} else {
		w.Write([]byte(`{"err":"` + errId + `","detail":"`))
		w.Write([]byte(errMsg))
		w.Write([]byte(`"}`))
	}
}

func GetRandomStr(len int) string {
	var nonce = make([]byte, len/2)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(err)
	}
	return hex.EncodeToString(nonce)
}

func GetIp(req *http.Request) string {
	remoteIp := strings.Split(req.RemoteAddr, ":")[0]
	userIp := req.Header.Get("X-Real-IP")
	if len(userIp) > 0 {
		if trustedProxies, ok := ConfigMap.Load("trusted_proxies"); ok {
			if len(trustedProxies.(string)) > 0 {
				for _, trustIp := range strings.Split(trustedProxies.(string), ",") {
					if remoteIp == trustIp || trustIp == "0.0.0.0" {
						return userIp
					}
				}
			}
		}
	}
	return remoteIp
}

func isInnerIp(ip string) bool {
	if ip == "127.0.0.1" {
		return true
	}
	ipArr := strings.Split(ip, ",")
	switch true {
	case ipArr[0] == "10":
		return true
	case ipArr[0] == "172" && (ipArr[1] == "16" || ipArr[1] == "17" || ipArr[1] == "18" || ipArr[1] == "19" || ipArr[1] == "20" || ipArr[1] == "21" || ipArr[1] == "22" || ipArr[1] == "23" || ipArr[1] == "24" || ipArr[1] == "25" || ipArr[1] == "26" || ipArr[1] == "27" || ipArr[1] == "28" || ipArr[1] == "29" || ipArr[1] == "30" || ipArr[1] == "31"):
		return true
	case ipArr[0] == "192" && ipArr[1] == "168":
		return true
	default:
		return false
	}
}

func SendDingdingText(title, msg string, userid string, accessToken string) bool {
	postUrl := fmt.Sprintf("https://oapi.dingtalk.com/topapi/message/corpconversation/asyncsend_v2?access_token=%s", accessToken)
	dingdingAgentId, _ := ConfigMap.Load("dingding_agent_id")
	respBody, _, err := FetchDingApi(postUrl, `{"agent_id":"`+dingdingAgentId.(string)+`","msg":{"msgtype":"markdown","markdown":{"title":"`+title+`","text":"`+msg+`"}},"userid_list":"`+userid+`","to_all_user":false}`, "POST")

	loger.Println(string(respBody))

	if err != nil {
		//w.WriteHeader(http.StatusInternalServerError)
		//EchoJs(w, "err:26", respBody)
		loger.Println(err.Error())
		return false
	}

	return true
}

func doTwoFactorAuthenticationCheck(w http.ResponseWriter, req *http.Request, ssoUserInfo SsoUserInfoStruct, isGet bool, userIp string, userAgent string) string {
	if _, ok := MemTrustIpMap.Load(userIp); !ok {
		twoFactorAuthentication, _ := ConfigMap.Load("two_factor_authentication")
		if twoFactorAuthentication.(string) == "on" {
			if isGet == true {
				loger.Println(fmt.Sprintf("twoFactorAuthenticationCheck step 1 echoForm ip: %s, userAgent: %s", userIp, userAgent))
				echoTwoFactorAuthenticationForm(w, ssoUserInfo)
				return "exit"
			} else {
				if err := req.ParseForm(); err != nil {
					EchoJs(w, "err:20", nil)
					fmt.Println(err.Error())
					return "exit"
				}
				twoFactorAuthenticationCheck := checkTwoFactorAuthenticationForm(w, req, ssoUserInfo)
				if twoFactorAuthenticationCheck == "--0--" { // 异常
					return "exit"
				}
				if twoFactorAuthenticationCheck != "--success--" { // 验证失败
					loger.Println(fmt.Sprintf("twoFactorAuthenticationCheck step 2 fail \"%s\" ip: %s, userAgent: %s", twoFactorAuthenticationCheck, userIp, userAgent))
					EchoJs(w, twoFactorAuthenticationCheck, nil)
					now := time.Now().Unix()
					twoFactorAuthenticationBlockDuration, _ := ConfigMap.Load("two_factor_authentication_block_duration")
					twoFactorAuthenticationBlockDurationInt, err := strconv.Atoi(twoFactorAuthenticationBlockDuration.(string))
					if err == nil && twoFactorAuthenticationBlockDurationInt > 0 {
						MemForbiddenMap.Store(ssoUserInfo.SsoDingdingOpenId, ForbiddenStruct{SsoName: ssoUserInfo.SsoName, SsoContactType: ssoUserInfo.SsoContactType, SsoMobile: ssoUserInfo.SsoMobile, Expired: now + int64(twoFactorAuthenticationBlockDurationInt)})
						//MemForbiddenMap.Store(userIp, ForbiddenStruct{SsoName: ssoUserInfo.SsoName, SsoContactType: ssoUserInfo.SsoContactType, SsoMobile: ssoUserInfo.SsoMobile, Expired: now + int64(twoFactorAuthenticationBlockDurationInt)})
					}
					return "exit"
				}
				loger.Println(fmt.Sprintf("twoFactorAuthenticationCheck step 2 success ip: %s, userAgent: %s", userIp, userAgent))
			}
		}
	}
	return ""
}

func echoTwoFactorAuthenticationForm(w http.ResponseWriter, ssoUserInfo SsoUserInfoStruct) {
	ssoUserByte, err := json.Marshal(ssoUserInfo)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		EchoJs(w, "err:19:1", nil)
		return
	}
	title, _ := ConfigMap.Load("title")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<title>` + title.(string) + `</title>`))
	client := &http.Client{}
	postUrl, ok := ConfigMap.Load("two_factor_authentication_url")
	if !ok {
		EchoJs(w, "err:32:1", nil)
		return
	}
	var q = url.Values{}
	q.Set("sso_step", "1")
	q.Add("sso_user", string(ssoUserByte))
	queryStr := q.Encode()
	request, err := http.NewRequest("POST", postUrl.(string), strings.NewReader(queryStr))
	if err != nil {
		EchoJs(w, "err:32:2", nil)
		loger.Println(err.Error())
		return
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		EchoJs(w, "err:32:3", nil)
		loger.Println(err.Error())
		return
	}
	defer response.Body.Close()
	body, _ := ioutil.ReadAll(response.Body)
	w.Write(body)
}

func checkTwoFactorAuthenticationForm(w http.ResponseWriter, req *http.Request, ssoUserInfo SsoUserInfoStruct) string {
	ssoUserByte, err := json.Marshal(ssoUserInfo)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		EchoJs(w, "err:19:2", nil)
		return "--0--"
	}
	title, _ := ConfigMap.Load("title")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<title>` + title.(string) + `</title>`))
	client := &http.Client{}
	postUrl, ok := ConfigMap.Load("two_factor_authentication_url")
	if !ok {
		EchoJs(w, "err:32:4", nil)
		return "--0--"
	}
	req.Form.Set("sso_step", "2")
	req.Form.Set("sso_user", string(ssoUserByte))
	queryStr := req.Form.Encode()
	request, err := http.NewRequest("POST", postUrl.(string), strings.NewReader(queryStr))
	if err != nil {
		EchoJs(w, "err:32:5", nil)
		loger.Println(err.Error())
		return "--0--"
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		EchoJs(w, "err:32:6", nil)
		loger.Println(err.Error())
		return "--0--"
	}
	defer response.Body.Close()
	body, _ := ioutil.ReadAll(response.Body)
	if string(body) == "success" {
		return "--success--"
	}
	return string(body)
}

var sharedValue = 1000
var valueMutex sync.Mutex

func GetCounterInt() int {
	valueMutex.Lock()
	sharedValue += 1
	if sharedValue == 10000 {
		sharedValue = 1000
	}
	valueMutex.Unlock()

	return sharedValue
}

var ConfigMap sync.Map

func ReadFile() {
	goReadFile()
}

func goReadFile() {
	b, err := ioutil.ReadFile("config.ini") // just pass the file name
	if err != nil {
		panic("config.ini read error")
	}

	str := string(b) // convert content to a 'string'

	var sendContentArr = strings.Split(strings.Trim(strings.Trim(strings.Trim(strings.Replace(str, "\r\n", "\n", -1), "\n"), "\t"), " "), "\n")
	if len(sendContentArr) < 2 {
		panic("config.ini read error")
	}

	for _, line := range sendContentArr {
		kv := strings.SplitN(line, " = ", 2)

		if len(kv) == 2 {
			ConfigMap.Store(kv[0], kv[1])
			//fmt.Println(kv[0], kv[1])
		}
	}

	time.Sleep(time.Second * 5)

	go goReadFile()
}
