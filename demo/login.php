<?php
const HOST = "abc.com"; // 扫码服务的服务器HOST
const ORIGIN = "https://abc.com"; // 扫码服务的服务器ORIGIN
const FETCH_URL = "https://abc.com"; // 扫码服务的服务器域名
const SCAN_URI = "/bms-sso/scan"; // 扫码服务的扫码的页面
const FETCH_URI = "/bms-sso/fetch-by-ticket"; // 扫码服务的查询ticket的地址
const FETCH_IP = "106.8.8.8"; // 扫码服务的服务器ip
const FETCH_PORT = 80; // 扫码服务的服务器端口
const TTL = "300"; // 单次授权后ticket过期时间
const RENEW = false; // 每次调用时, 是否续延ticket的过期时间, 需要服务端同时配置开启才会生效

if ($_SERVER['REQUEST_METHOD'] == 'GET') {
$scanUrl = FETCH_URL . SCAN_URI;
$origin = ORIGIN;
$ttl = TTL;
echo '<meta charset="UTF-8"/>';
echo <<<JS
<style>
body{font-size:28px;}
.form-control {
	height: 34px;
	padding: 6px 12px;
	font-size: 14px;
	line-height: 1.428571429;
	color: #555555;
	vertical-align: middle;
	border: 1px solid #cccccc;
	border-radius: 4px;
	-webkit-box-shadow: inset 0 1px 1px rgba(0, 0, 0, 0.075);
	box-shadow: inset 0 1px 1px rgba(0, 0, 0, 0.075);
	-webkit-transition: border-color ease-in-out .15s, box-shadow ease-in-out .15s;
	transition: border-color ease-in-out .15s, box-shadow ease-in-out .15s;
}
.form-control:focus {
	border-color: #66afe9;
	outline: 0;
	-webkit-box-shadow: inset 0 1px 1px rgba(0,0,0,.075), 0 0 8px rgba(102, 175, 233, 0.6);
	box-shadow: inset 0 1px 1px rgba(0,0,0,.075), 0 0 8px rgba(102, 175, 233, 0.6);
}
</style>
<script>
function dingdingOpen(dingdingUrl) {
	window.open(dingdingUrl, 'dingdingScan', 'height=580, width=608, top=0, left=0, toolbar=no, menubar=no, scrollbars=no, resizable=no, location=no, status=no')
}
var handleMessage = function (event) {
    var origin = event.origin;
    //console.log(origin)
    //console.log(event.data)
    if (origin == window.origin || origin == "$origin") {
    	if (event.data.err == "0") {
            document.getElementById("ticket").value = event.data.detail.sso_ticket
            document.getElementById("nick_name").value = event.data.detail.sso_dingding_nick_name
  		} else {
  			document.getElementById("tip").innerHTML = "错误提示: " + event.data.detail + "<br>错误编号: " + event.data.err
  		}
    }
};
if (typeof window.addEventListener != 'undefined') {
    window.addEventListener('message', handleMessage, false);
} else if (typeof window.attachEvent != 'undefined') {
    window.attachEvent('onmessage', handleMessage);
}
</script>
<form action="" method="post">
<table style="margin:100px auto; width: 550px; border-collapse: collapse; border: 3px solid #CCC" cellpadding="15" cellspacing="15">
<tr>
    <td style="padding-left: 50px;">Nick Name: </td>
    <td><input type="text" class="form-control" style="width: 170px;" maxlength="56" autocomplete="off" placeholder="" value="" name="nick_name" id="nick_name" /></td>
</tr>
<tr>
    <td style="padding-left: 50px;">Ticket: </td>
    <td>
        <input type="text" class="form-control" style="width: 170px;" autocomplete="off" placeholder="" value="" name="ticket" id="ticket" />
        <input type="button" value="扫码" class="form-control" style="cursor:pointer;width:100px" onclick="dingdingOpen('$scanUrl?auto=1&ttl=$ttl')" />
    </td>
</tr>
<tr>
    <td colspan="2" style="text-align: center">
        <input type="submit" value="提 交" class="form-control" style="cursor:pointer;width:100px" />
    </td>
</tr>
</table>
<div id="tip"></div>
</form>
JS;
}

if ($_SERVER['REQUEST_METHOD'] == 'POST') {
    if (empty($_POST['ticket'])) {
        echo '<meta charset="UTF-8"/>';
        die("请先扫码");
    }
    $ticket = $_POST['ticket'];
    $json = fetchSsoUserInfoByTicket($ticket, RENEW, $_SERVER['REMOTE_ADDR'], $_SERVER['HTTP_USER_AGENT']);
    $dingdingUserData = json_decode($json, true);
        
    if (!$dingdingUserData) {
        echo '<meta charset="UTF-8"/>';
        die("请正确登录");
    }
    if ($dingdingUserData['err'] !== "0") { // 校验失败
        echo '<meta charset="UTF-8"/>远端返回: <br>';
        die($json);
    }

    if ($dingdingUserData['detail']['sso_contact_type'] === 0) { // 内部员工
        $name = $dingdingUserData['detail']['sso_name']; // 员工真实姓名
        $deptName = $dingdingUserData['detail']['sso_dept_name']; // 员工的部门名称
        $mobile = $dingdingUserData['detail']['sso_mobile']; // 公司留存的员工手机号
/*
`json:"sso_name"`               // 用户名
`json:"sso_contact_type"`       // 0 内部联系人     1 外部联系人
`json:"sso_mobile"`             // 手机号
`json:"sso_avatar"`             // 头像链接
`json:"sso_job_title"`          // 工作岗位名称
`json:"sso_state_code"`         // 国家编号
`json:"sso_company_name"`       // 公司名字  外部联系人才有
`json:"sso_email"`              // 邮箱  外部联系人才有
`json:"sso_follower_user_id"`   // 负责人userId  外部联系人才有
`json:"sso_follower_user"`      // 负责人结构体  外部联系人才有
`json:"sso_address"`            // 联系地址  外部联系人才有
`json:"sso_remark"`             // 备注  外部联系人才有
`json:"sso_dingding_union_id"`  // 钉钉 公司内 分配的用户id
`json:"sso_dingding_user_id"`   // 钉钉 分配的用户id
`json:"sso_dingding_open_id"`   // 钉钉 分配的open id
`json:"sso_dingding_nick_name"` // 钉钉 设置的用户昵称
`json:"dingding_raw"`           // 原始调用api过来的数据
*/

        echo '<meta charset="UTF-8"/>';
        die("欢迎您, $name, 登录成功! <br>远端返回: <br>" . ($json));
        
    } else {
        echo '<meta charset="UTF-8"/>';
        die("外部联系人不允许登录!<br>远端返回: <br>" . ($json));
    }
}

function fetchSsoUserInfoByTicket($ticket, $renew, $ip, $userAgent, $type = "socket") {
    list($exec_time_usec_1, $exec_time_sec_1) = explode(' ', microtime());
    $php_exec_time_start = ((float)$exec_time_usec_1 + (float)$exec_time_sec_1);
    
    if ($type == 'curl') {
        $vars = array(
            "sso_ticket" => "$ticket",
            "client_ip" => "$ip",
            "user_agent" => "$userAgent",
            "renew" => (($renew === true) ? "1" : "0")
        );
        $ch = curl_init();

        curl_setopt($ch, CURLOPT_URL, FETCH_URL . FETCH_URI);
        curl_setopt($ch, CURLOPT_CONNECTTIMEOUT, 3);
        curl_setopt($ch, CURLOPT_TIMEOUT, 3);
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, 10);
        curl_setopt($ch, CURLOPT_POST, 1);
        curl_setopt($ch, CURLOPT_HEADER, false);
        curl_setopt(
            $ch,
            CURLOPT_HTTPHEADER,
            array(
                'Host: ' . HOST
            )
        );

        curl_setopt($ch, CURLOPT_POSTFIELDS, http_build_query($vars));
        $file_contents = curl_exec($ch);
        curl_close($ch);
        
        list($exec_time_usec_2, $exec_time_sec_2) = explode(' ', microtime());
        $exec_time_end = ((float)$exec_time_usec_2 + (float)$exec_time_sec_2);
        $a2 = round(($exec_time_end - $php_exec_time_start) * 1000, 5);
        echo "\r\n" . '[[[ Time: ' . $a2 . ' ms ]]]<br>';

        return $file_contents;
    }

    if ($type == 'socket') {
        start:
        $fp = @pfsockopen(FETCH_IP, FETCH_PORT, $errno, $errstr, 3);

        if ($fp) {
            $vars = array(
                "sso_ticket" => "$ticket",
                "client_ip" => "$ip",
                "user_agent" => "$userAgent",
                "renew" => (($renew === true) ? "1" : "0")
            );
            $content = http_build_query($vars);

            $request = "POST " . FETCH_URI . " HTTP/1.1\r\n" .
                        "Host: " . HOST . "\r\n" .
                        "Content-Type: application/x-www-form-urlencoded\r\n" .
                        "Content-Length: ".strlen($content)."\r\n" .
                        "Connection: keep-alive\r\n" .
                        "\r\n" .
                        $content;

            $writeBytes = @fwrite($fp, $request);

            if (isset($writeBytes) && $writeBytes == 0) {
                //die('error');
                fclose($fp);

                goto start;
            }

            $isChuncked = null;
            $contentLenth = 0;
            do {
                $header = fgets($fp);
                if (strpos($header, "Transfer-Encoding: chunked") === 0) {
                    $isChuncked = true;
                } elseif (strpos($header, "Content-Length:") === 0) {
                    $isChuncked = false;
                    $contentLenth = intval(trim(str_replace("Content-Length:", "", $header)));
                }
                if ($header == "\r\n") {
                    break;
                }
                // var_dump($header);
            } while(true);

            if ($isChuncked === false) {
                $readed = 0;
                $i = 0;
                $body = '';
                do {
                    // echo ++$i . " ";
                    $needRead = $contentLenth - $readed;
                    $body .= fread($fp, $needRead);
                    $readed = strlen($body);

                    if ($readed == $contentLenth) {
                        break;
                    }
                } while(true);
                
                list($exec_time_usec_2, $exec_time_sec_2) = explode(' ', microtime());
                $exec_time_end = ((float)$exec_time_usec_2 + (float)$exec_time_sec_2);
                $a2 = round(($exec_time_end - $php_exec_time_start) * 1000, 5);
                echo "\r\n" . '[[[ Time: ' . $a2 . ' ms ]]]<br>';
                return $body;
                
            } else {
                $readed = 0;
                $i = 0;
                $body = '';
                do {
                    //echo ++$i . " ";
                    $chunckLength = hexdec(trim(fgets($fp)));
                    $readed += $chunckLength;
                    if ($chunckLength == 0) {
                        fgets($fp); // 忽略一次空行
                        break;
                    }
                    //var_dump($chunckLength);
                    $body .=  fread($fp, $chunckLength);
                    fgets($fp); // 忽略一次空行
                } while(true);

                //var_dump($readed);
                list($exec_time_usec_2, $exec_time_sec_2) = explode(' ', microtime());
                $exec_time_end = ((float)$exec_time_usec_2 + (float)$exec_time_sec_2);
                $a2 = round(($exec_time_end - $php_exec_time_start) * 1000, 5);
                echo "\r\n" . '[[[ Time: ' . $a2 . ' ms ]]]<br>';
                
                return $body;
            }
        } else {
            die("fetchSsoUserInfoByTicket Error: " . json_encode($errstr) . " (#" . json_encode($errno) . ")");
        }
    }
}
