package main

import (
	"testing"
)

func Test_randomPort(t *testing.T) {
	type args struct {
		start int
		end   int
		count int
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "1-10,9", args: args{
				start: 1,
				end:   10,
				count: 9,
			},
		},
		{
			name: "1-10,8", args: args{
				start: 1,
				end:   10,
				count: 8,
			},
		},
		{
			name: "33250-33300,5", args: args{
				start: 33250,
				end:   33300,
				count: 5,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPorts, err := randomPort(tt.args.start, tt.args.end, tt.args.count)
			t.Logf("randomPort() = %v, %v", gotPorts, err)
		})
	}
}
