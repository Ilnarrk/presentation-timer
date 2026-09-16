package singleinstance

import "errors"

var ErrAlreadyRunning = errors.New("таймер уже запущен")
