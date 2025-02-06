package core

import (
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

type StructA struct {
	Foo      string `bson:"foo"`
	FooInt   int
	FooSlice []string `bson:"get"`
	FooMap   map[string]string
}

type StructB struct {
	Bar string `bson:"get"`
}

type StructC struct {
	InstanceOfA    StructA  `bson:"instanceOfA"`
	PtrInstanceOfB *StructB `bson:"ptrInstanceOfB"`
}

func fillStructs() StructC {
	strA := StructA{
		Foo:      "fooString",
		FooInt:   0,
		FooSlice: []string{"fooSlice1", "fooSlice2"},
		FooMap:   map[string]string{"fooMapKey": "fooMapValue"},
	}
	strB := &StructB{"barString"}

	strC := StructC{
		InstanceOfA:    strA,
		PtrInstanceOfB: strB,
	}

	return strC
}

func TestGetFieldsAndNamesByTag(t *testing.T) {
	var depth = 1

	type args struct {
		fieldName map[string]interface{}
		tag       string
		key       string
		s         interface{}
		depth     *int
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "Get by tag",
			args: args{
				fieldName: make(map[string]interface{}),
				tag:       "get",
				key:       "bson",
				s:         fillStructs(),
				depth:     &depth,
			},
		},
		{
			name: "No tag found",
			args: args{
				fieldName: make(map[string]interface{}),
				tag:       "bar",
				key:       "bson",
				s:         fillStructs(),
				depth:     &depth,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			GetFieldsAndNamesByTag(tt.args.fieldName, tt.args.tag, tt.args.key, tt.args.s, tt.args.depth)
			switch name := tt.name; name {
			case "Get by tag":
				{
					assert.True(t, len(tt.args.fieldName) == 2)
					assert.True(t, tt.args.fieldName["Bar"] == "barString")
					assert.True(t, reflect.DeepEqual(tt.args.fieldName["FooSlice"], []string{"fooSlice1", "fooSlice2"}))
				}
			case "No tag found":
				{
					assert.True(t, len(tt.args.fieldName) == 0)
				}
			}
		})
	}
}
