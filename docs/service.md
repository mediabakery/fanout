# Systemd Service

How to setup systemd service for sender and receiver. It is important that the KEY is a random 32 char long secret.

## sender

```ini
; /etc/systemd/system/fanout_sender.service
; Example config
[Unit]
Description=Fanout sender
After=network.target

[Service]
Environment="ADDR=:8124"
Environment="TARGET_URL=http://192.168.123.123:8123/api/webhook/8ada5f84d269f9f08c660745954f6408"
Environment="KEY=12345678901234567890123456789012"
Environment="NATS_URL=connect.ngs.global"
Environment="CLIENT_NAME=iot-sender"
Environment="NATS_CREDENTIALS=/opt/fanout/creds/NGS-iot-sender.creds"
Environment="SUBJECT=iot.foo"

ExecStart=/opt/fanout/sender
Restart=on-failure
RestartSec=5
User={USER}

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable fanout_sender.service
sudo systemctl start fanout_sender.service
sudo systemctl status fanout_sender.service
```


## receiver

```ini
; /etc/systemd/system/fanout_receiver.service
; Example config
[Unit]
Description=Fanout receiver
After=network.target

[Service]
Environment="TARGET_URL=http://192.168.123.123:8123/api/webhook/8ada5f84d269f9f08c660745954f6408"
Environment="KEY=12345678901234567890123456789012"
Environment="NATS_URL=connect.ngs.global"
Environment="CLIENT_NAME=iot-receiver"
Environment="NATS_CREDENTIALS=/opt/fanout/creds/NGS-iot-receiver.creds"
Environment="JS_NAME=foo"
Environment="SUBJECT=iot.foo"

ExecStart=/opt/fanout/receiver
Restart=on-failure
RestartSec=5
User={USER}

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable fanout_receiver.service
sudo systemctl start fanout_receiver.service
sudo systemctl status fanout_receiver.service
```