package imagefsd

import "testing"

func TestDaemonCreateOpt_OverwriteOSSConfig(t *testing.T) {
	tests := []struct {
		name string
		opts *DaemonCreateOpt
		want bool
	}{
		{
			name: "All OSS fields provided",
			opts: &DaemonCreateOpt{
				Endpoint:     "oss-cn-hangzhou.aliyuncs.com",
				Bucket:       "test-bucket",
				ObjectPrefix: "prefix/",
			},
			want: true,
		},
		{
			name: "Missing endpoint",
			opts: &DaemonCreateOpt{
				Bucket:       "test-bucket",
				ObjectPrefix: "prefix/",
			},
			want: false,
		},
		{
			name: "Missing bucket",
			opts: &DaemonCreateOpt{
				Endpoint:     "oss-cn-hangzhou.aliyuncs.com",
				ObjectPrefix: "prefix/",
			},
			want: false,
		},
		{
			name: "Missing object prefix",
			opts: &DaemonCreateOpt{
				Endpoint: "oss-cn-hangzhou.aliyuncs.com",
				Bucket:   "test-bucket",
			},
			want: false,
		},
		{
			name: "All fields empty",
			opts: &DaemonCreateOpt{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.overwriteOSSConfig(); got != tt.want {
				t.Errorf("overwriteOSSConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
