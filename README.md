# retv

一个简单的 m3u 代理

Apple TV 的某些 iptv 播放器(底层使用的 VLC)地址中有 IPv6 地址时无法播放，因此有了这个简单代理

## caddy 配置

```
:80 {
  bind 192.168.1.200
  route {
    reverse_proxy /r/* 127.0.0.1:18090

    file_server /* {
      root  /var/www/html
    }
  }
}
```

## 样例 m3u 列表配置

```
#EXTINF:-1 tvg-id="CHC动作电影" tvg-name="CHC动作电影" tvg-logo="" group-title="其他",CHC动作电影
http://192.168.1.200/r/[2409:8087:7000:20:1000::22]:6060/yinhe/2/ch00000090990000002055/index.m3u8?virtualDomain=yinhe.live_hls.zte.com
#EXTINF:-1 tvg-id="CHC家庭影院" tvg-name="CHC家庭影院" tvg-logo="" group-title="其他",CHC家庭影院
http://192.168.1.200/r/[2409:8087:7000:20:1000::22]:6060/yinhe/2/ch00000090990000002085/index.m3u8?virtualDomain=yinhe.live_hls.zte.com
#EXTINF:-1 tvg-id="CHC高清电影" tvg-name="CHC高清电影" tvg-logo="" group-title="其他",CHC高清电影
http://192.168.1.200/r/[2409:8087:7000:20:1000::22]:6060/yinhe/2/ch00000090990000002065/index.m3u8?virtualDomain=yinhe.live_hls.zte.com
```

## systemd 服务文件

```
[Unit]
Description=Reverse Proxy for IPTV Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
PIDFile=/run/retv.pid
ExecStart=/usr/bin/retv -l 127.0.0.1:18090
Restart=always
LimitNOFILE=4096
RestartSec=3

[Install]
WantedBy=multi-user.target
```

## 感谢

@fanmingming 的项目 https://github.com/fanmingming/live
