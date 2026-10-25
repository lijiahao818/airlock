package main

import "errors"

// errHeld 表示另一个 Airlock 进程正在使用同一个数据目录。
// 两个进程同时运行时，后启动的一方会删除前一方的沙箱，并覆盖 cabins.json。
var errHeld = errors.New("另一个 Airlock 正在运行")
