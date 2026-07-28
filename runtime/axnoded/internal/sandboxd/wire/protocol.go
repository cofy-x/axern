package wire

const (
	ProtocolVersion = 1

	PathHealth       = "/healthz"
	PathReady        = "/readyz"
	PathCapabilities = "/capabilities"
	PathDiagnostics  = "/diagnostics"
	PathMounts       = "/mounts"
	PathPorts        = "/ports"
	PathProbe        = "/probe"
	PathStatus       = "/status"

	PathFilesPrefix           = "/files/"
	PathFileStat              = "/files/stat"
	PathFileList              = "/files/list"
	PathFileRead              = "/files/read"
	PathFileExists            = "/files/exists"
	PathFileWrite             = "/files/write"
	PathFileMkdir             = "/files/mkdir"
	PathFileRemove            = "/files/remove"
	PathFileCopy              = "/files/copy"
	PathFileMove              = "/files/move"
	PathFileChmod             = "/files/chmod"
	PathFileTouch             = "/files/touch"
	PathArchiveUpload         = "/files/archive/upload"
	PathArchiveDownload       = "/files/archive/download"
	PathProcesses             = "/processes"
	PathProcessesPrefix       = "/processes/"
	PathComputerUsePrefix     = "/computer-use/"
	PathComputerUseStatus     = "/computer-use/status"
	PathComputerUseScreenshot = "/computer-use/screenshot"
	PathComputerUseDisplay    = "/computer-use/display"
	PathComputerUseMouse      = "/computer-use/mouse"
	PathComputerUseKeyboard   = "/computer-use/keyboard"
	PathBrowserPrefix         = "/browser/"
	PathBrowserStatus         = "/browser/status"
	PathBrowserOpen           = "/browser/open"
	PathBrowserClose          = "/browser/close"
	PathBrowserNavigate       = "/browser/navigate"
	PathBrowserResize         = "/browser/resize"
	PathBrowserClick          = "/browser/click"
	PathBrowserType           = "/browser/type"
	PathBrowserWait           = "/browser/wait"
)

const (
	ContentTypeJSON = "application/json"
)
