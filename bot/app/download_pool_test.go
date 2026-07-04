package app

import "testing"

func TestResolveDownloadWorkerPoolSize(t *testing.T) {
	tests := []struct {
		name                string
		configured          int
		downloadConcurrency int
		waitLimit           int
		globalLimit         int
		want                int
	}{
		{name: "explicit below observable queue minimum", configured: 3, downloadConcurrency: 8, waitLimit: 20, globalLimit: 24, want: 24},
		{name: "explicit above minimum", configured: 32, downloadConcurrency: 8, waitLimit: 20, globalLimit: 24, want: 32},
		{name: "global default", downloadConcurrency: 4, waitLimit: 20, globalLimit: 24, want: 24},
		{name: "not below download concurrency", downloadConcurrency: 8, waitLimit: 2, globalLimit: 4, want: 8},
		{name: "wait plus concurrency fallback", downloadConcurrency: 4, waitLimit: 6, want: 10},
		{name: "hard fallback", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDownloadWorkerPoolSize(tt.configured, tt.downloadConcurrency, tt.waitLimit, tt.globalLimit)
			if got != tt.want {
				t.Fatalf("resolveDownloadWorkerPoolSize() = %d, want %d", got, tt.want)
			}
		})
	}
}
