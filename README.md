# retv

一个简单的 m3u 代理

Apple TV 的某些 iptv 播放器(底层使用的 VLC)地址中有 IPv6 地址时无法播放，因此有了这个简单代理

## caddy 配置

```
:80 {
  bind 192.168.1.200
  route {
    reverse_proxy /r/* 127.0.0.1:18090
    reverse_proxy /rtp/* 127.0.0.1:18090

    file_server /* {
      root  /var/www/html
    }
  }
}
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

## 参考资料

1. https://en.wikipedia.org/wiki/RTP_payload_formats
