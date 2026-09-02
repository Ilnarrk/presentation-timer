//go:build windows

package audio

import (
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

func ListDevices() ([]Device, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return nil, err
	}
	defer ole.CoUninitialize()

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		return nil, err
	}

	var collection *wca.IMMDeviceCollection
	if err := enumerator.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &collection); err != nil {
		return nil, err
	}

	var count uint32
	if err := collection.GetCount(&count); err != nil {
		return nil, err
	}

	devices := []Device{{ID: "default", Name: "Системное устройство по умолчанию"}}
	for i := uint32(0); i < count; i++ {
		var endpoint *wca.IMMDevice
		if err := collection.Item(i, &endpoint); err != nil {
			continue
		}

		var id string
		if err := endpoint.GetId(&id); err != nil {
			continue
		}

		name, err := deviceFriendlyName(endpoint)
		if err != nil || name == "" {
			name = fmt.Sprintf("Устройство %d", i+1)
		}

		devices = append(devices, Device{ID: id, Name: name})
	}

	return devices, nil
}

func deviceFriendlyName(device *wca.IMMDevice) (string, error) {
	var store *wca.IPropertyStore
	if err := device.OpenPropertyStore(0, &store); err != nil {
		return "", err
	}

	var variant wca.PROPVARIANT
	if err := store.GetValue(&wca.PKEY_Device_FriendlyName, &variant); err != nil {
		return "", err
	}

	return variant.String(), nil
}

func resolveDevice(enumerator *wca.IMMDeviceEnumerator, deviceID string) (*wca.IMMDevice, error) {
	if deviceID == "" || deviceID == "default" {
		var device *wca.IMMDevice
		if err := enumerator.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &device); err != nil {
			return nil, err
		}
		return device, nil
	}

	var collection *wca.IMMDeviceCollection
	if err := enumerator.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &collection); err != nil {
		return nil, err
	}

	var count uint32
	if err := collection.GetCount(&count); err != nil {
		return nil, err
	}

	for i := uint32(0); i < count; i++ {
		var endpoint *wca.IMMDevice
		if err := collection.Item(i, &endpoint); err != nil {
			continue
		}
		var id string
		if err := endpoint.GetId(&id); err != nil {
			continue
		}
		if id == deviceID {
			return endpoint, nil
		}
	}

	return nil, fmt.Errorf("audio device not found")
}
