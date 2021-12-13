<?php

// var_dump($_GET);
// var_dump($_POST);
// var_dump(file_get_contents("php://input"));

if ($_SERVER['REQUEST_METHOD'] !== 'POST') exit;
if ($_SERVER['REMOTE_ADDR'] !== '127.0.0.1') die("ip not 127.0.0.1");
if (!isset($_POST['sso_step'])) die("must set sso_step");
if (!isset($_POST['sso_user'])) die("must set sso_user");

if ($_POST['sso_step'] !== '1' && $_POST['sso_step'] !== '2') die("sso_step must be 1 or 2");
$sso_user = json_decode($_POST['sso_user'], true);
if (!$sso_user) die("decode sso_user error");

if ($_POST['sso_step'] === '1') {
    if (isset($sso_user['sso_name']))
    echo <<<HTML
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
<body>
<form action="" method="post">
<p style="font-size:14px;font-weight:bold">您好，{$sso_user['sso_name']}</p>
<p style="font-size:14px;font-weight:bold">系统检测到您不在常用ip登录，请回答如下问题，回答错误账号锁定一小时</p>
<p style="font-size:12px;font-weight:bold">请输入三个字<span style="color:#FF6600">*</span>: <input type="text" class="form-control" style="width: 70px;" maxlength="3" autocomplete="off" placeholder="" value="" name="txtCode" /></p>
<p style="font-size:12px;font-weight:bold">请输入颜色<span style="color:#FF6600">*</span>: <input type="text" class="form-control" style="width: 40px;" maxlength="1" autocomplete="off" placeholder="" value="" name="colorCode" /></p>
<div style="height:10px;overflow:hidden">&nbsp;</div>
<input type="submit" value="提 交" class="form-control" style="cursor:pointer;width:100px" />
</form>
</body>
HTML;
    
}
if ($_POST['sso_step'] === '2') {
    echo "success";
    //echo "校验失败";
}
