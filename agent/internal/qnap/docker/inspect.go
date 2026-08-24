package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type inspectDocument struct {
	ID              string                 `json:"Id"`
	Name            string                 `json:"Name"`
	Path            string                 `json:"Path"`
	Args            []string               `json:"Args"`
	Created         string                 `json:"Created"`
	Config          inspectConfig          `json:"Config"`
	HostConfig      inspectHostConfig      `json:"HostConfig"`
	State           inspectState           `json:"State"`
	NetworkSettings inspectNetworkSettings `json:"NetworkSettings"`
	Mounts          []inspectMount         `json:"Mounts"`
	raw             map[string]any
}

type inspectConfig struct {
	Hostname     string                    `json:"Hostname"`
	Domainname   string                    `json:"Domainname"`
	User         string                    `json:"User"`
	AttachStdin  bool                      `json:"AttachStdin"`
	AttachStdout bool                      `json:"AttachStdout"`
	AttachStderr bool                      `json:"AttachStderr"`
	Tty          bool                      `json:"Tty"`
	OpenStdin    bool                      `json:"OpenStdin"`
	StdinOnce    bool                      `json:"StdinOnce"`
	Env          []string                  `json:"Env"`
	Cmd          []string                  `json:"Cmd"`
	Image        string                    `json:"Image"`
	Volumes      map[string]any            `json:"Volumes"`
	WorkingDir   string                    `json:"WorkingDir"`
	Entrypoint   []string                  `json:"Entrypoint"`
	Labels       map[string]string         `json:"Labels"`
	StopSignal   string                    `json:"StopSignal"`
	StopTimeout  *int                      `json:"StopTimeout"`
	Shell        []string                  `json:"Shell"`
	Healthcheck  *inspectHealthcheck       `json:"Healthcheck"`
	ExposedPorts map[string]map[string]any `json:"ExposedPorts"`
}

type inspectHealthcheck struct {
	Test          []string `json:"Test"`
	Interval      int64    `json:"Interval"`
	Timeout       int64    `json:"Timeout"`
	Retries       int      `json:"Retries"`
	StartPeriod   int64    `json:"StartPeriod"`
	StartInterval int64    `json:"StartInterval"`
}

type inspectHealthState struct {
	Status        string             `json:"Status"`
	FailingStreak int                `json:"FailingStreak"`
	Log           []inspectHealthLog `json:"Log"`
}

type inspectHealthLog struct {
	Start    string `json:"Start"`
	End      string `json:"End"`
	ExitCode int    `json:"ExitCode"`
	Output   string `json:"Output"`
}

type inspectState struct {
	Status     string              `json:"Status"`
	Running    bool                `json:"Running"`
	Paused     bool                `json:"Paused"`
	Restarting bool                `json:"Restarting"`
	OOMKilled  bool                `json:"OOMKilled"`
	Dead       bool                `json:"Dead"`
	ExitCode   int                 `json:"ExitCode"`
	Error      string              `json:"Error"`
	StartedAt  string              `json:"StartedAt"`
	FinishedAt string              `json:"FinishedAt"`
	Health     *inspectHealthState `json:"Health"`
}

type inspectHostConfig struct {
	Binds                []string                        `json:"Binds"`
	Mounts               []inspectMount                  `json:"Mounts"`
	ContainerIDFile      string                          `json:"ContainerIDFile"`
	LogConfig            inspectLogConfig                `json:"LogConfig"`
	NetworkMode          string                          `json:"NetworkMode"`
	PortBindings         map[string][]inspectPortBinding `json:"PortBindings"`
	RestartPolicy        inspectRestartPolicy            `json:"RestartPolicy"`
	AutoRemove           bool                            `json:"AutoRemove"`
	VolumeDriver         string                          `json:"VolumeDriver"`
	VolumesFrom          []string                        `json:"VolumesFrom"`
	CapAdd               []string                        `json:"CapAdd"`
	CapDrop              []string                        `json:"CapDrop"`
	CgroupnsMode         string                          `json:"CgroupnsMode"`
	DNS                  []string                        `json:"Dns"`
	DNSOptions           []string                        `json:"DnsOptions"`
	DNSSearch            []string                        `json:"DnsSearch"`
	ExtraHosts           []string                        `json:"ExtraHosts"`
	GroupAdd             []string                        `json:"GroupAdd"`
	IpcMode              string                          `json:"IpcMode"`
	Links                []string                        `json:"Links"`
	OomScoreAdj          int                             `json:"OomScoreAdj"`
	PIDMode              string                          `json:"PidMode"`
	Privileged           bool                            `json:"Privileged"`
	PublishAllPorts      bool                            `json:"PublishAllPorts"`
	ReadonlyRootfs       bool                            `json:"ReadonlyRootfs"`
	SecurityOpt          []string                        `json:"SecurityOpt"`
	StorageOpt           map[string]string               `json:"StorageOpt"`
	Tmpfs                map[string]string               `json:"Tmpfs"`
	UTSMode              string                          `json:"UTSMode"`
	UsernsMode           string                          `json:"UsernsMode"`
	ShmSize              int64                           `json:"ShmSize"`
	Sysctls              map[string]string               `json:"Sysctls"`
	Runtime              string                          `json:"Runtime"`
	Isolation            string                          `json:"Isolation"`
	CPUCount             int64                           `json:"CpuCount"`
	CPUPercent           int64                           `json:"CpuPercent"`
	CPUShares            int64                           `json:"CpuShares"`
	Memory               int64                           `json:"Memory"`
	NanoCPUs             int64                           `json:"NanoCpus"`
	CPUPeriod            int64                           `json:"CpuPeriod"`
	CPUQuota             int64                           `json:"CpuQuota"`
	CPURealtimePeriod    int64                           `json:"CpuRealtimePeriod"`
	CPURealtimeRuntime   int64                           `json:"CpuRealtimeRuntime"`
	CpusetCPUs           string                          `json:"CpusetCpus"`
	CpusetMems           string                          `json:"CpusetMems"`
	CgroupParent         string                          `json:"CgroupParent"`
	BlkioWeight          uint16                          `json:"BlkioWeight"`
	MemoryReservation    int64                           `json:"MemoryReservation"`
	MemorySwap           int64                           `json:"MemorySwap"`
	MemorySwappiness     *int64                          `json:"MemorySwappiness"`
	OomKillDisable       *bool                           `json:"OomKillDisable"`
	PidsLimit            int64                           `json:"PidsLimit"`
	Init                 *bool                           `json:"Init"`
	InitPath             string                          `json:"InitPath"`
	KernelMemory         int64                           `json:"KernelMemory"`
	KernelMemoryTCP      int64                           `json:"KernelMemoryTCP"`
	Devices              []inspectDevice                 `json:"Devices"`
	DeviceCgroupRules    []string                        `json:"DeviceCgroupRules"`
	Ulimits              []inspectUlimit                 `json:"Ulimits"`
	IOMaximumIOps        uint64                          `json:"IOMaximumIOps"`
	IOMaximumBandwidth   uint64                          `json:"IOMaximumBandwidth"`
	BlkioWeightDevice    []inspectWeightDevice           `json:"BlkioWeightDevice"`
	BlkioDeviceReadBPS   []inspectThrottleDevice         `json:"BlkioDeviceReadBps"`
	BlkioDeviceWriteBPS  []inspectThrottleDevice         `json:"BlkioDeviceWriteBps"`
	BlkioDeviceReadIOPS  []inspectThrottleDevice         `json:"BlkioDeviceReadIOps"`
	BlkioDeviceWriteIOPS []inspectThrottleDevice         `json:"BlkioDeviceWriteIOps"`
}

type inspectLogConfig struct {
	Type   string            `json:"Type"`
	Config map[string]string `json:"Config"`
}

type inspectRestartPolicy struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount"`
}

type inspectPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type inspectDevice struct {
	PathOnHost        string `json:"PathOnHost"`
	PathInContainer   string `json:"PathInContainer"`
	CgroupPermissions string `json:"CgroupPermissions"`
}

type inspectUlimit struct {
	Name string `json:"Name"`
	Soft int64  `json:"Soft"`
	Hard int64  `json:"Hard"`
}

type inspectWeightDevice struct {
	Path   string `json:"Path"`
	Weight uint16 `json:"Weight"`
}

type inspectThrottleDevice struct {
	Path string `json:"Path"`
	Rate uint64 `json:"Rate"`
}

type inspectMount struct {
	Type          string                `json:"Type"`
	Name          string                `json:"Name"`
	Source        string                `json:"Source"`
	Destination   string                `json:"Destination"`
	Driver        string                `json:"Driver"`
	Mode          string                `json:"Mode"`
	RW            bool                  `json:"RW"`
	Propagation   string                `json:"Propagation"`
	Consistency   string                `json:"Consistency"`
	BindOptions   *inspectBindOptions   `json:"BindOptions"`
	VolumeOptions *inspectVolumeOptions `json:"VolumeOptions"`
	TmpfsOptions  *inspectTmpfsOptions  `json:"TmpfsOptions"`
}

type inspectBindOptions struct {
	Propagation      string `json:"Propagation"`
	NonRecursive     bool   `json:"NonRecursive"`
	CreateMountpoint bool   `json:"CreateMountpoint"`
}

type inspectVolumeOptions struct {
	NoCopy  bool              `json:"NoCopy"`
	Labels  map[string]string `json:"Labels"`
	Subpath string            `json:"Subpath"`
}

type inspectTmpfsOptions struct {
	SizeBytes int64  `json:"SizeBytes"`
	Mode      uint32 `json:"Mode"`
}

type inspectNetworkSettings struct {
	Networks map[string]inspectNetworkEndpoint `json:"Networks"`
}

type inspectNetworkEndpoint struct {
	EndpointID          string            `json:"EndpointID"`
	Gateway             string            `json:"Gateway"`
	IPAddress           string            `json:"IPAddress"`
	IPPrefixLen         int               `json:"IPPrefixLen"`
	IPv6Gateway         string            `json:"IPv6Gateway"`
	GlobalIPv6Address   string            `json:"GlobalIPv6Address"`
	GlobalIPv6PrefixLen int               `json:"GlobalIPv6PrefixLen"`
	MacAddress          string            `json:"MacAddress"`
	NetworkID           string            `json:"NetworkID"`
	DriverOpts          map[string]string `json:"DriverOpts"`
	Aliases             []string          `json:"Aliases"`
}

func parseInspectOutput(stdout string) ([]inspectDocument, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(stdout)))
	var out []inspectDocument
	for {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse docker inspect output: %w", err)
		}
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		if raw[0] == '[' {
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, fmt.Errorf("parse docker inspect array: %w", err)
			}
			for _, item := range items {
				doc, err := decodeInspectDocument(item)
				if err != nil {
					return nil, err
				}
				out = append(out, doc)
			}
			continue
		}
		doc, err := decodeInspectDocument(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

func decodeInspectDocument(raw json.RawMessage) (inspectDocument, error) {
	var doc inspectDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return inspectDocument{}, fmt.Errorf("decode docker inspect item: %w", err)
	}
	if err := json.Unmarshal(raw, &doc.raw); err != nil {
		return inspectDocument{}, fmt.Errorf("decode docker inspect raw item: %w", err)
	}
	return doc, nil
}

func (s Service) inspectContainers(ctx context.Context, refs ...string) ([]inspectDocument, error) {
	args := []string{"inspect", "--format", "{{json .}}"}
	args = append(args, refs...)
	result, err := s.Run(ctx, args, 45)
	if err != nil {
		return nil, err
	}
	return parseInspectOutput(result.Stdout)
}
