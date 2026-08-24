package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ReconstructionStatus string

const (
	ReconstructionEquivalent ReconstructionStatus = "equivalent"
	ReconstructionLossy      ReconstructionStatus = "lossy"
)

type Reconstruction struct {
	Status      ReconstructionStatus `json:"status"`
	Equivalent  bool                 `json:"equivalent"`
	Inspect     map[string]any       `json:"inspect"`
	DockerRun   []string             `json:"docker_run"`
	Compose     ComposeService       `json:"compose"`
	Unsupported []string             `json:"unsupported"`
	LossReport  []ReconstructionLoss `json:"loss_report"`
}

type ReconstructionLoss struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ComposeService is the service-level subset emitted by reconstruction. The
// fields intentionally use Compose's familiar names so the result can be
// reviewed or embedded in a compose service without claiming that omitted
// fields were recreated.
type ComposeService struct {
	Image                string              `json:"image,omitempty"`
	ContainerName        string              `json:"container_name,omitempty"`
	Hostname             string              `json:"hostname,omitempty"`
	Domainname           string              `json:"domainname,omitempty"`
	Entrypoint           []string            `json:"entrypoint,omitempty"`
	Command              []string            `json:"command,omitempty"`
	Environment          []string            `json:"environment,omitempty"`
	Labels               map[string]string   `json:"labels,omitempty"`
	User                 string              `json:"user,omitempty"`
	WorkingDir           string              `json:"working_dir,omitempty"`
	Restart              string              `json:"restart,omitempty"`
	Volumes              []string            `json:"volumes,omitempty"`
	VolumesFrom          []string            `json:"volumes_from,omitempty"`
	VolumeDriver         string              `json:"volume_driver,omitempty"`
	Ports                []string            `json:"ports,omitempty"`
	NetworkMode          string              `json:"network_mode,omitempty"`
	Networks             []ComposeNetwork    `json:"networks,omitempty"`
	MacAddress           string              `json:"mac_address,omitempty"`
	Privileged           bool                `json:"privileged,omitempty"`
	ReadOnly             bool                `json:"read_only,omitempty"`
	AutoRemove           bool                `json:"auto_remove,omitempty"`
	Init                 bool                `json:"init,omitempty"`
	TTY                  bool                `json:"tty,omitempty"`
	StdinOpen            bool                `json:"stdin_open,omitempty"`
	CapAdd               []string            `json:"cap_add,omitempty"`
	CapDrop              []string            `json:"cap_drop,omitempty"`
	Devices              []string            `json:"devices,omitempty"`
	DeviceCgroupRules    []string            `json:"device_cgroup_rules,omitempty"`
	GroupAdd             []string            `json:"group_add,omitempty"`
	SecurityOpt          []string            `json:"security_opt,omitempty"`
	ExtraHosts           []string            `json:"extra_hosts,omitempty"`
	DNS                  []string            `json:"dns,omitempty"`
	DNSOptions           []string            `json:"dns_opt,omitempty"`
	DNSSearch            []string            `json:"dns_search,omitempty"`
	Links                []string            `json:"links,omitempty"`
	Tmpfs                []string            `json:"tmpfs,omitempty"`
	Sysctls              map[string]string   `json:"sysctls,omitempty"`
	Runtime              string              `json:"runtime,omitempty"`
	Isolation            string              `json:"isolation,omitempty"`
	ShmSize              string              `json:"shm_size,omitempty"`
	Ipc                  string              `json:"ipc,omitempty"`
	PID                  string              `json:"pid,omitempty"`
	UTS                  string              `json:"uts,omitempty"`
	UsernsMode           string              `json:"userns_mode,omitempty"`
	CgroupnsMode         string              `json:"cgroup,omitempty"`
	CgroupParent         string              `json:"cgroup_parent,omitempty"`
	CPUShares            int64               `json:"cpu_shares,omitempty"`
	CPUs                 string              `json:"cpus,omitempty"`
	CPUPeriod            int64               `json:"cpu_period,omitempty"`
	CPUQuota             int64               `json:"cpu_quota,omitempty"`
	CPURealtimePeriod    int64               `json:"cpu_rt_period,omitempty"`
	CPURealtimeRuntime   int64               `json:"cpu_rt_runtime,omitempty"`
	CpusetCPUs           string              `json:"cpuset,omitempty"`
	CpusetMems           string              `json:"cpuset_mems,omitempty"`
	Memory               string              `json:"mem_limit,omitempty"`
	MemoryReservation    string              `json:"mem_reservation,omitempty"`
	MemorySwap           string              `json:"memswap_limit,omitempty"`
	MemorySwappiness     *int64              `json:"mem_swappiness,omitempty"`
	OomKillDisable       *bool               `json:"oom_kill_disable,omitempty"`
	OomScoreAdj          int                 `json:"oom_score_adj,omitempty"`
	PidsLimit            int64               `json:"pids_limit,omitempty"`
	KernelMemory         string              `json:"kernel_memory,omitempty"`
	BlkioWeight          uint16              `json:"blkio_weight,omitempty"`
	BlkioWeightDevice    []string            `json:"blkio_weight_device,omitempty"`
	BlkioDeviceReadBPS   []string            `json:"device_read_bps,omitempty"`
	BlkioDeviceWriteBPS  []string            `json:"device_write_bps,omitempty"`
	BlkioDeviceReadIOPS  []string            `json:"device_read_iops,omitempty"`
	BlkioDeviceWriteIOPS []string            `json:"device_write_iops,omitempty"`
	StorageOpt           map[string]string   `json:"storage_opt,omitempty"`
	Ulimits              map[string]string   `json:"ulimits,omitempty"`
	Logging              *ComposeLogging     `json:"logging,omitempty"`
	Healthcheck          *ComposeHealthcheck `json:"healthcheck,omitempty"`
	StopSignal           string              `json:"stop_signal,omitempty"`
	StopGracePeriod      string              `json:"stop_grace_period,omitempty"`
}

type ComposeNetwork struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

type ComposeLogging struct {
	Driver  string            `json:"driver,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

type ComposeHealthcheck struct {
	Test          []string `json:"test,omitempty"`
	Interval      string   `json:"interval,omitempty"`
	Timeout       string   `json:"timeout,omitempty"`
	Retries       int      `json:"retries,omitempty"`
	StartPeriod   string   `json:"start_period,omitempty"`
	StartInterval string   `json:"start_interval,omitempty"`
}

func (s Service) Reconstruct(ctx context.Context, name string) (Reconstruction, error) {
	if strings.TrimSpace(name) == "" {
		return Reconstruction{}, fmt.Errorf("container name is required")
	}
	documents, err := s.inspectContainers(ctx, name)
	if err != nil {
		return Reconstruction{}, err
	}
	if len(documents) == 0 {
		return Reconstruction{}, fmt.Errorf("docker inspect returned no container for %q", name)
	}
	return buildReconstruction(documents[0], s.RedactSecrets), nil
}

// ReconstructContainer is an operation-oriented alias for Reconstruct.
func (s Service) ReconstructContainer(ctx context.Context, name string) (Reconstruction, error) {
	return s.Reconstruct(ctx, name)
}

// BuildReconstruction makes the reconstruction decision available to tests
// and future adapters without requiring a Docker binary.
func BuildReconstruction(raw map[string]any, redactSecrets bool) (Reconstruction, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Reconstruction{}, fmt.Errorf("encode inspect document: %w", err)
	}
	document, err := decodeInspectDocument(encoded)
	if err != nil {
		return Reconstruction{}, err
	}
	return buildReconstruction(document, redactSecrets), nil
}

func buildReconstruction(document inspectDocument, redactSecrets bool) Reconstruction {
	r := Reconstruction{
		Status:      ReconstructionLossy,
		Equivalent:  false,
		Inspect:     cloneJSONMap(document.raw),
		DockerRun:   []string{"docker", "run"},
		Compose:     ComposeService{Environment: []string{}, Volumes: []string{}, Ports: []string{}},
		Unsupported: []string{},
		LossReport:  []ReconstructionLoss{},
	}
	if redactSecrets {
		redactInspectEnvironment(r.Inspect)
	}

	add := func(args ...string) { r.DockerRun = append(r.DockerRun, args...) }
	if name := strings.TrimPrefix(document.Name, "/"); name != "" {
		add("--name", name)
		r.Compose.ContainerName = name
	}
	if document.Config.Hostname != "" {
		add("--hostname", document.Config.Hostname)
		r.Compose.Hostname = document.Config.Hostname
	}
	if document.Config.Domainname != "" {
		add("--domainname", document.Config.Domainname)
		r.Compose.Domainname = document.Config.Domainname
	}
	if document.Config.User != "" {
		add("--user", document.Config.User)
		r.Compose.User = document.Config.User
	}
	if document.Config.WorkingDir != "" {
		add("--workdir", document.Config.WorkingDir)
		r.Compose.WorkingDir = document.Config.WorkingDir
	}
	if len(document.Config.Entrypoint) == 1 {
		add("--entrypoint", document.Config.Entrypoint[0])
		r.Compose.Entrypoint = append([]string(nil), document.Config.Entrypoint...)
	} else if len(document.Config.Entrypoint) > 1 {
		r.addLoss("Config.Entrypoint", "docker run --entrypoint accepts one executable and cannot preserve a multi-argument entrypoint exactly")
	}
	if len(document.Config.Cmd) > 0 {
		r.Compose.Command = append([]string(nil), document.Config.Cmd...)
	}
	if document.Config.Image == "" {
		r.addLoss("Config.Image", "inspect did not contain an image reference")
	} else {
		r.Compose.Image = document.Config.Image
	}

	for _, env := range document.Config.Env {
		if redactSecrets && isSecretEnv(env) {
			key := envKey(env)
			r.addLoss("Config.Env["+key+"]", "secret environment value was redacted and cannot be reconstructed")
			continue
		}
		add("--env", env)
		r.Compose.Environment = append(r.Compose.Environment, env)
	}
	if len(document.Config.Labels) > 0 {
		r.Compose.Labels = cloneStringMap(document.Config.Labels)
		for _, key := range sortedKeys(document.Config.Labels) {
			add("--label", key+"="+document.Config.Labels[key])
		}
	}
	if document.Config.Tty {
		add("--tty")
		r.Compose.TTY = true
	}
	if document.Config.OpenStdin {
		add("--interactive")
		r.Compose.StdinOpen = true
	}
	if document.Config.StdinOnce {
		r.addLoss("Config.StdinOnce", "Docker Compose and docker run do not preserve the one-shot attach behavior")
	}
	if document.Config.StopSignal != "" {
		add("--stop-signal", document.Config.StopSignal)
		r.Compose.StopSignal = document.Config.StopSignal
	}
	if document.Config.StopTimeout != nil {
		add("--stop-timeout", strconv.Itoa(*document.Config.StopTimeout))
		r.Compose.StopGracePeriod = secondsString(*document.Config.StopTimeout)
	}

	appendHealthcheck(&r, add, document.Config.Healthcheck)
	appendHostConfig(&r, add, document)
	appendMounts(&r, add, document)
	appendPorts(&r, add, document.HostConfig.PortBindings)
	appendNetworks(&r, add, document)
	appendConfigVolumes(&r, add, document)

	if document.Config.Image != "" {
		r.DockerRun = append(r.DockerRun, document.Config.Image)
	}
	if len(document.Config.Cmd) > 0 {
		r.DockerRun = append(r.DockerRun, document.Config.Cmd...)
	}

	collectUnknownFields(&r, document)
	if len(r.LossReport) == 0 {
		r.Status = ReconstructionEquivalent
		r.Equivalent = true
	}
	return r
}

func (r *Reconstruction) addLoss(field, reason string) {
	for _, loss := range r.LossReport {
		if loss.Field == field {
			return
		}
	}
	r.LossReport = append(r.LossReport, ReconstructionLoss{Field: field, Reason: reason})
	r.Unsupported = append(r.Unsupported, field)
}

func appendHealthcheck(r *Reconstruction, add func(...string), check *inspectHealthcheck) {
	if check == nil {
		return
	}
	compose := &ComposeHealthcheck{Test: append([]string(nil), check.Test...)}
	r.Compose.Healthcheck = compose
	if healthcheckDisabled(check) {
		add("--no-healthcheck")
		return
	}
	if len(check.Test) < 2 {
		r.addLoss("Config.Healthcheck.Test", "healthcheck test is missing its command")
	} else if strings.EqualFold(check.Test[0], "CMD-SHELL") && len(check.Test) == 2 {
		add("--health-cmd", check.Test[1])
	} else if strings.EqualFold(check.Test[0], "CMD") {
		r.addLoss("Config.Healthcheck.Test", "exec-form healthcheck cannot be represented losslessly by docker run --health-cmd")
	} else {
		r.addLoss("Config.Healthcheck.Test", "unknown healthcheck test form")
	}
	if check.Interval > 0 {
		value := durationString(check.Interval)
		add("--health-interval", value)
		compose.Interval = value
	}
	if check.Timeout > 0 {
		value := durationString(check.Timeout)
		add("--health-timeout", value)
		compose.Timeout = value
	}
	if check.Retries > 0 {
		value := strconv.Itoa(check.Retries)
		add("--health-retries", value)
		compose.Retries = check.Retries
	}
	if check.StartPeriod > 0 {
		value := durationString(check.StartPeriod)
		add("--health-start-period", value)
		compose.StartPeriod = value
	}
	if check.StartInterval > 0 {
		value := durationString(check.StartInterval)
		add("--health-start-interval", value)
		compose.StartInterval = value
	}
}

func appendHostConfig(r *Reconstruction, add func(...string), document inspectDocument) {
	host := document.HostConfig
	compose := &r.Compose
	if host.AutoRemove {
		add("--rm")
		compose.AutoRemove = true
	}
	if host.ContainerIDFile != "" {
		r.addLoss("HostConfig.ContainerIDFile", "cidfile is a client-side docker run concern and is not preserved by Compose")
	}
	if host.RestartPolicy.Name != "" && host.RestartPolicy.Name != "no" {
		restart := host.RestartPolicy.Name
		if restart == "on-failure" && host.RestartPolicy.MaximumRetryCount > 0 {
			restart += ":" + strconv.Itoa(host.RestartPolicy.MaximumRetryCount)
		}
		add("--restart", restart)
		compose.Restart = restart
	}
	if host.VolumeDriver != "" {
		add("--volume-driver", host.VolumeDriver)
		compose.VolumeDriver = host.VolumeDriver
	}
	for _, value := range host.VolumesFrom {
		add("--volumes-from", value)
		compose.VolumesFrom = append(compose.VolumesFrom, value)
	}
	for _, value := range host.CapAdd {
		add("--cap-add", value)
		compose.CapAdd = append(compose.CapAdd, value)
	}
	for _, value := range host.CapDrop {
		add("--cap-drop", value)
		compose.CapDrop = append(compose.CapDrop, value)
	}
	if host.Privileged {
		add("--privileged")
		compose.Privileged = true
	}
	if host.PublishAllPorts {
		add("--publish-all")
	}
	if host.ReadonlyRootfs {
		add("--read-only")
		compose.ReadOnly = true
	}
	if host.Init != nil && *host.Init {
		add("--init")
		compose.Init = true
	}
	if host.InitPath != "" {
		r.addLoss("HostConfig.InitPath", "custom init binary path is not preserved by docker run --init or Compose")
	}
	if host.CgroupnsMode != "" {
		add("--cgroupns", host.CgroupnsMode)
		compose.CgroupnsMode = host.CgroupnsMode
	}
	if host.CgroupParent != "" {
		add("--cgroup-parent", host.CgroupParent)
		compose.CgroupParent = host.CgroupParent
	}
	if host.IpcMode != "" {
		add("--ipc", host.IpcMode)
		compose.Ipc = host.IpcMode
	}
	if host.PIDMode != "" {
		add("--pid", host.PIDMode)
		compose.PID = host.PIDMode
	}
	if host.UTSMode != "" {
		add("--uts", host.UTSMode)
		compose.UTS = host.UTSMode
	}
	if host.UsernsMode != "" {
		add("--userns", host.UsernsMode)
		compose.UsernsMode = host.UsernsMode
	}
	if host.Runtime != "" {
		add("--runtime", host.Runtime)
		compose.Runtime = host.Runtime
	}
	if host.Isolation != "" {
		add("--isolation", host.Isolation)
		compose.Isolation = host.Isolation
	}
	if host.ShmSize != 0 {
		value := strconv.FormatInt(host.ShmSize, 10)
		add("--shm-size", value)
		compose.ShmSize = value
	}
	for _, value := range host.DNS {
		add("--dns", value)
		compose.DNS = append(compose.DNS, value)
	}
	for _, value := range host.DNSOptions {
		add("--dns-option", value)
		compose.DNSOptions = append(compose.DNSOptions, value)
	}
	for _, value := range host.DNSSearch {
		add("--dns-search", value)
		compose.DNSSearch = append(compose.DNSSearch, value)
	}
	for _, value := range host.ExtraHosts {
		add("--add-host", value)
		compose.ExtraHosts = append(compose.ExtraHosts, value)
	}
	for _, value := range host.GroupAdd {
		add("--group-add", value)
		compose.GroupAdd = append(compose.GroupAdd, value)
	}
	for _, value := range host.Links {
		add("--link", value)
		compose.Links = append(compose.Links, value)
	}
	for _, value := range host.SecurityOpt {
		add("--security-opt", value)
		compose.SecurityOpt = append(compose.SecurityOpt, value)
	}
	for _, key := range sortedKeys(host.StorageOpt) {
		value := key + "=" + host.StorageOpt[key]
		add("--storage-opt", value)
		if compose.StorageOpt == nil {
			compose.StorageOpt = map[string]string{}
		}
		compose.StorageOpt[key] = host.StorageOpt[key]
	}
	for _, key := range sortedKeys(host.Sysctls) {
		value := key + "=" + host.Sysctls[key]
		add("--sysctl", value)
		if compose.Sysctls == nil {
			compose.Sysctls = map[string]string{}
		}
		compose.Sysctls[key] = host.Sysctls[key]
	}
	for _, key := range sortedKeys(host.Tmpfs) {
		value := key
		if host.Tmpfs[key] != "" {
			value += ":" + host.Tmpfs[key]
		}
		add("--tmpfs", value)
		compose.Tmpfs = append(compose.Tmpfs, value)
	}
	for _, device := range host.Devices {
		value := device.PathOnHost + ":" + device.PathInContainer
		if device.CgroupPermissions != "" {
			value += ":" + device.CgroupPermissions
		}
		add("--device", value)
		compose.Devices = append(compose.Devices, value)
	}
	for _, value := range host.DeviceCgroupRules {
		add("--device-cgroup-rule", value)
		compose.DeviceCgroupRules = append(compose.DeviceCgroupRules, value)
	}
	for _, ulimit := range host.Ulimits {
		value := ulimit.Name + "=" + strconv.FormatInt(ulimit.Soft, 10) + ":" + strconv.FormatInt(ulimit.Hard, 10)
		add("--ulimit", value)
		if compose.Ulimits == nil {
			compose.Ulimits = map[string]string{}
		}
		compose.Ulimits[ulimit.Name] = strconv.FormatInt(ulimit.Soft, 10) + ":" + strconv.FormatInt(ulimit.Hard, 10)
	}

	appendResourceFlags(r, add, host)
	appendLogging(r, add, host.LogConfig)
	if host.NetworkMode != "" {
		compose.NetworkMode = host.NetworkMode
		if host.NetworkMode != "default" {
			add("--network", host.NetworkMode)
		}
	}
}

func appendResourceFlags(r *Reconstruction, add func(...string), host inspectHostConfig) {
	compose := &r.Compose
	if host.CPUShares != 0 {
		add("--cpu-shares", strconv.FormatInt(host.CPUShares, 10))
		compose.CPUShares = host.CPUShares
	}
	if host.NanoCPUs != 0 {
		value := formatNanoCPUs(host.NanoCPUs)
		add("--cpus", value)
		compose.CPUs = value
	}
	if host.CPUPeriod != 0 {
		add("--cpu-period", strconv.FormatInt(host.CPUPeriod, 10))
		compose.CPUPeriod = host.CPUPeriod
	}
	if host.CPUQuota != 0 {
		add("--cpu-quota", strconv.FormatInt(host.CPUQuota, 10))
		compose.CPUQuota = host.CPUQuota
	}
	if host.CPURealtimePeriod != 0 {
		add("--cpu-rt-period", strconv.FormatInt(host.CPURealtimePeriod, 10))
		compose.CPURealtimePeriod = host.CPURealtimePeriod
	}
	if host.CPURealtimeRuntime != 0 {
		add("--cpu-rt-runtime", strconv.FormatInt(host.CPURealtimeRuntime, 10))
		compose.CPURealtimeRuntime = host.CPURealtimeRuntime
	}
	if host.CpusetCPUs != "" {
		add("--cpuset-cpus", host.CpusetCPUs)
		compose.CpusetCPUs = host.CpusetCPUs
	}
	if host.CpusetMems != "" {
		add("--cpuset-mems", host.CpusetMems)
		compose.CpusetMems = host.CpusetMems
	}
	if host.Memory != 0 {
		value := strconv.FormatInt(host.Memory, 10)
		add("--memory", value)
		compose.Memory = value
	}
	if host.MemoryReservation != 0 {
		value := strconv.FormatInt(host.MemoryReservation, 10)
		add("--memory-reservation", value)
		compose.MemoryReservation = value
	}
	if host.MemorySwap != 0 {
		value := strconv.FormatInt(host.MemorySwap, 10)
		add("--memory-swap", value)
		compose.MemorySwap = value
	}
	if host.MemorySwappiness != nil {
		add("--memory-swappiness", strconv.FormatInt(*host.MemorySwappiness, 10))
		value := *host.MemorySwappiness
		compose.MemorySwappiness = &value
	}
	if host.OomKillDisable != nil && *host.OomKillDisable {
		add("--oom-kill-disable")
		value := true
		compose.OomKillDisable = &value
	}
	if host.OomScoreAdj != 0 {
		add("--oom-score-adj", strconv.Itoa(host.OomScoreAdj))
		compose.OomScoreAdj = host.OomScoreAdj
	}
	if host.PidsLimit != 0 {
		add("--pids-limit", strconv.FormatInt(host.PidsLimit, 10))
		compose.PidsLimit = host.PidsLimit
	}
	if host.KernelMemory != 0 {
		value := strconv.FormatInt(host.KernelMemory, 10)
		add("--kernel-memory", value)
		compose.KernelMemory = value
	}
	if host.BlkioWeight != 0 {
		add("--blkio-weight", strconv.Itoa(int(host.BlkioWeight)))
		compose.BlkioWeight = host.BlkioWeight
	}
	for _, device := range host.BlkioWeightDevice {
		value := device.Path + ":" + strconv.Itoa(int(device.Weight))
		add("--blkio-weight-device", value)
		compose.BlkioWeightDevice = append(compose.BlkioWeightDevice, value)
	}
	for _, device := range host.BlkioDeviceReadBPS {
		value := device.Path + ":" + strconv.FormatUint(device.Rate, 10)
		add("--device-read-bps", value)
		compose.BlkioDeviceReadBPS = append(compose.BlkioDeviceReadBPS, value)
	}
	for _, device := range host.BlkioDeviceWriteBPS {
		value := device.Path + ":" + strconv.FormatUint(device.Rate, 10)
		add("--device-write-bps", value)
		compose.BlkioDeviceWriteBPS = append(compose.BlkioDeviceWriteBPS, value)
	}
	for _, device := range host.BlkioDeviceReadIOPS {
		value := device.Path + ":" + strconv.FormatUint(device.Rate, 10)
		add("--device-read-iops", value)
		compose.BlkioDeviceReadIOPS = append(compose.BlkioDeviceReadIOPS, value)
	}
	for _, device := range host.BlkioDeviceWriteIOPS {
		value := device.Path + ":" + strconv.FormatUint(device.Rate, 10)
		add("--device-write-iops", value)
		compose.BlkioDeviceWriteIOPS = append(compose.BlkioDeviceWriteIOPS, value)
	}
	if host.CPUCount != 0 {
		r.addLoss("HostConfig.CpuCount", "CPU count is platform-specific and has no portable Compose equivalent")
	}
	if host.CPUPercent != 0 {
		r.addLoss("HostConfig.CpuPercent", "CPU percent is platform-specific and has no portable Compose equivalent")
	}
	if host.IOMaximumIOps != 0 {
		r.addLoss("HostConfig.IOMaximumIOps", "I/O maximum IOPS is platform-specific and is not represented by this Compose service")
	}
	if host.IOMaximumBandwidth != 0 {
		r.addLoss("HostConfig.IOMaximumBandwidth", "I/O maximum bandwidth is platform-specific and is not represented by this Compose service")
	}
}

func appendLogging(r *Reconstruction, add func(...string), config inspectLogConfig) {
	if config.Type == "" && len(config.Config) == 0 {
		return
	}
	logging := &ComposeLogging{Driver: config.Type, Options: cloneStringMap(config.Config)}
	r.Compose.Logging = logging
	if config.Type != "" {
		add("--log-driver", config.Type)
	}
	for _, key := range sortedKeys(config.Config) {
		add("--log-opt", key+"="+config.Config[key])
	}
}

func appendMounts(r *Reconstruction, add func(...string), document inspectDocument) {
	seen := map[string]bool{}
	for _, bind := range document.HostConfig.Binds {
		add("--volume", bind)
		r.Compose.Volumes = append(r.Compose.Volumes, bind)
		if key := bindTargetKey(bind); key != "" {
			seen[key] = true
		}
	}
	mounts := append([]inspectMount{}, document.Mounts...)
	mounts = append(mounts, document.HostConfig.Mounts...)
	for _, mount := range mounts {
		key := mount.Source + "\x00" + mount.Destination
		if seen[key] {
			continue
		}
		value, composeValue, ok := renderMount(mount)
		if !ok {
			r.addLoss("Mounts["+mount.Destination+"]", "mount contains options that are not represented losslessly by the emitted docker run and Compose fields")
			continue
		}
		add("--mount", value)
		r.Compose.Volumes = append(r.Compose.Volumes, composeValue)
		seen[key] = true
	}
}

func appendConfigVolumes(r *Reconstruction, add func(...string), document inspectDocument) {
	seen := map[string]bool{}
	for _, value := range r.Compose.Volumes {
		if target := volumeTarget(value); target != "" {
			seen[target] = true
		}
	}
	for target := range document.Config.Volumes {
		if !seen[target] {
			add("--volume", target)
			r.Compose.Volumes = append(r.Compose.Volumes, target)
		}
	}
}

func appendPorts(r *Reconstruction, add func(...string), ports map[string][]inspectPortBinding) {
	for _, containerPort := range sortedKeys(ports) {
		for _, binding := range ports[containerPort] {
			value := formatPortBinding(containerPort, binding)
			add("--publish", value)
			r.Compose.Ports = append(r.Compose.Ports, value)
		}
	}
}

func appendNetworks(r *Reconstruction, add func(...string), document inspectDocument) {
	networks := document.NetworkSettings.Networks
	if len(networks) == 0 {
		return
	}
	names := sortedKeys(networks)
	for _, name := range names {
		endpoint := networks[name]
		add("--network", name)
		entry := ComposeNetwork{Name: name, Aliases: append([]string(nil), endpoint.Aliases...)}
		for _, alias := range endpoint.Aliases {
			add("--network-alias", alias)
		}
		r.Compose.Networks = append(r.Compose.Networks, entry)
		if endpoint.IPAddress != "" || endpoint.GlobalIPv6Address != "" || endpoint.Gateway != "" || endpoint.IPv6Gateway != "" {
			r.addLoss("NetworkSettings.Networks."+name, "static address or gateway state is inspect data that this reconstruction does not apply per network")
		}
		if endpoint.MacAddress != "" {
			if len(names) == 1 {
				add("--mac-address", endpoint.MacAddress)
				r.Compose.MacAddress = endpoint.MacAddress
			} else {
				r.addLoss("NetworkSettings.Networks."+name+".MacAddress", "docker run cannot assign per-network MAC addresses for multiple attachments")
			}
		}
		if len(endpoint.DriverOpts) > 0 {
			r.addLoss("NetworkSettings.Networks."+name+".DriverOpts", "network endpoint driver options are not preserved by container reconstruction")
		}
	}
}

func renderMount(mount inspectMount) (string, string, bool) {
	if mount.Destination == "" || strings.ContainsAny(mount.Destination, ",\n") || strings.ContainsAny(mount.Source, ",\n") {
		return "", "", false
	}
	typeName := strings.ToLower(mount.Type)
	if typeName == "" {
		typeName = "bind"
	}
	if typeName != "bind" && typeName != "volume" && typeName != "tmpfs" {
		return "", "", false
	}
	parts := []string{"type=" + typeName}
	if mount.Source != "" {
		parts = append(parts, "source="+mount.Source)
	}
	parts = append(parts, "target="+mount.Destination)
	if !mount.RW {
		parts = append(parts, "readonly")
	}
	if mount.Propagation != "" {
		parts = append(parts, "bind-propagation="+mount.Propagation)
	}
	if mount.Consistency != "" {
		parts = append(parts, "consistency="+mount.Consistency)
	}
	if mount.BindOptions != nil && (mount.BindOptions.NonRecursive || mount.BindOptions.CreateMountpoint) {
		return "", "", false
	}
	if mount.VolumeOptions != nil && (mount.VolumeOptions.NoCopy || mount.VolumeOptions.Subpath != "" || len(mount.VolumeOptions.Labels) > 0) {
		return "", "", false
	}
	if mount.TmpfsOptions != nil && (mount.TmpfsOptions.SizeBytes != 0 || mount.TmpfsOptions.Mode != 0) {
		return "", "", false
	}
	composeValue := mount.Source + ":" + mount.Destination
	if mount.Source == "" {
		composeValue = mount.Destination
	}
	if !mount.RW {
		composeValue += ":ro"
	}
	return strings.Join(parts, ","), composeValue, true
}

func bindTargetKey(bind string) string {
	parts := strings.Split(bind, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "\x00" + parts[1]
}

func volumeTarget(value string) string {
	parts := strings.Split(value, ":")
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[1]
}

func formatPortBinding(containerPort string, binding inspectPortBinding) string {
	host := binding.HostPort
	if binding.HostIP != "" {
		if strings.Contains(binding.HostIP, ":") && !strings.HasPrefix(binding.HostIP, "[") {
			host = "[" + binding.HostIP + "]:" + host
		} else {
			host = binding.HostIP + ":" + host
		}
	}
	if host == "" {
		return containerPort
	}
	return host + ":" + containerPort
}

func collectUnknownFields(r *Reconstruction, document inspectDocument) {
	config, _ := document.raw["Config"].(map[string]any)
	for key, value := range config {
		if meaningfulJSON(value) && !recognizedConfigField(key) {
			r.addLoss("Config."+key, "inspect field is not represented by the reconstruction")
		}
	}
	host, _ := document.raw["HostConfig"].(map[string]any)
	for key, value := range host {
		if meaningfulJSON(value) && !recognizedHostConfigField(key) {
			r.addLoss("HostConfig."+key, "inspect field is not represented by the reconstruction")
		}
	}
}

func recognizedConfigField(key string) bool {
	switch strings.ToLower(key) {
	case "hostname", "domainname", "user", "attachstdin", "attachstdout", "attachstderr", "tty", "openstdin", "stdinonce", "env", "cmd", "image", "volumes", "workingdir", "entrypoint", "labels", "stopsignal", "stoptimeout", "shell", "healthcheck", "exposedports", "argsescaped":
		return true
	default:
		return false
	}
}

func recognizedHostConfigField(key string) bool {
	switch strings.ToLower(key) {
	case "binds", "mounts", "containeridfile", "logconfig", "networkmode", "portbindings", "restartpolicy", "autoremove", "volumedriver", "volumesfrom", "capadd", "capdrop", "cgroupnsmode", "dns", "dnsoptions", "dnssearch", "extrahosts", "groupadd", "ipcmode", "links", "oomscoreadj", "pidmode", "privileged", "publishallports", "readonlyrootfs", "securityopt", "storageopt", "tmpfs", "utsmode", "usernsmode", "shmsize", "sysctls", "runtime", "isolation", "cpucount", "cpupercent", "cpushares", "memory", "nanocpus", "cpuperiod", "cpuquota", "cpurealtimeperiod", "cpurealtimeruntime", "cpusetcpus", "cpusetmems", "cgroupparent", "blioweight", "memoryreservation", "memoryswap", "memoryswappiness", "oomkilldisable", "pidslimit", "init", "initpath", "kernelmemory", "kernelmemorytcp", "devices", "devicecgrouprules", "ulimits", "iomaximumiops", "iomaximumbandwidth", "blioweightdevice", "bliocdevicereadbps", "bliodevicewritebps", "bliodevicereadiops", "bliodevicewriteiops", "maskedpaths", "readonlypaths":
		return true
	default:
		return false
	}
}

func meaningfulJSON(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func durationString(nanoseconds int64) string {
	return time.Duration(nanoseconds).String()
}

func secondsString(seconds int) string {
	return (time.Duration(seconds) * time.Second).String()
}

func formatNanoCPUs(nanocpus int64) string {
	value := float64(nanocpus) / 1_000_000_000
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneJSONMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func redactInspectEnvironment(raw map[string]any) {
	config, ok := raw["Config"].(map[string]any)
	if !ok {
		return
	}
	env, ok := config["Env"].([]any)
	if !ok {
		return
	}
	for index, value := range env {
		entry, ok := value.(string)
		if !ok || !isSecretEnv(entry) {
			continue
		}
		env[index] = envKey(entry) + "=[REDACTED]"
	}
}

func isSecretEnv(value string) bool {
	key := strings.ToUpper(envKey(value))
	for _, marker := range []string{"PASSWORD", "PASSWD", "TOKEN", "SECRET", "API_KEY", "APIKEY", "PRIVATE_KEY", "CREDENTIAL", "AUTH"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func envKey(value string) string {
	if index := strings.IndexByte(value, '='); index >= 0 {
		return value[:index]
	}
	return value
}
