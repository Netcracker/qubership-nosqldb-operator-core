package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Netcracker/base/qubership-nosqldb-operator-core/pkg/constants"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Simple helper function to read an environment or return a default value
func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}

// Simple helper function to read an environment variable into integer or return a default value
func getEnvAsInt(name string, defaultVal int) int {
	valueStr := getEnv(name, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}

	return defaultVal
}

func getEnvAsBool(name string, defaultVal bool) bool {
	valueStr := getEnv(name, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}

	return defaultVal
}

// Max returns the larger of x or y.
func MaxInt(x, y int) int {
	if x < y {
		return y
	}
	return x
}

func MaxInt64(x, y int64) int64 {
	if x < y {
		return y
	}
	return x
}

func MaxInt32(x, y int32) int32 {
	if x < y {
		return y
	}
	return x
}

// Min returns the smaller of x or y.
func MinInt(x, y int) int {
	if x > y {
		return y
	}
	return x
}

func MakeRange(min, max int) []int {
	a := make([]int, max-min+1)
	for i := range a {
		a[i] = min + i
	}
	return a
}

func DeleteRuntimeObjectWithCheck(cl client.Client, object client.Object, checkTimeout int) error {
	err := DeleteRuntimeObject(cl, object)

	if errors.IsNotFound(err) {
		return nil
	}

	zeroObject := Zero(object)
	emptyObject := (zeroObject).(client.Object)
	return wait.PollImmediate(time.Second, time.Second*time.Duration(checkTimeout), func() (done bool, err error) {
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name: object.GetName(), Namespace: object.GetNamespace(),
		}, emptyObject)

		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			} else {
				return false, err
			}
		}

		return false, nil
	})
}

// TODO looks like it should be in helper
func DeleteRuntimeObject(client client.Client, object client.Object) error {
	err := client.Delete(context.TODO(), object)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func CreateOrUpdateRuntimeObject(kuberClient client.Client, scheme *runtime.Scheme, owner v12.Object, object client.Object, meta v12.ObjectMeta, forceUpdate bool) error {
	// Set reference.
	if owner != nil {
		properObj := (object).(v12.Object)
		if err := controllerutil.SetControllerReference(owner, properObj, scheme); err != nil {
			return err
		}
	}
	zeroObject := Zero(object)
	emptyObject := (zeroObject).(client.Object)
	err := kuberClient.Get(context.TODO(), types.NamespacedName{
		Name: meta.Name, Namespace: meta.Namespace,
	}, emptyObject)
	if err != nil && errors.IsNotFound(err) {
		err = kuberClient.Create(context.TODO(), object)
		if err != nil {
			newJsonString, errJson := json.MarshalIndent(object, "", "  ")
			if errJson != nil {
				newJsonString = []byte("Not able to parse object. Error: " + errJson.Error())
			}
			return &ExecutionError{
				Msg: fmt.Sprintf(
					"Resource creation is failed with the following message: %s\nNew resource: %s\n",
					err.Error(),
					string(newJsonString)),
			}
		}
	} else {
		if !reflect.DeepEqual(emptyObject, object) {
			err = kuberClient.Update(context.TODO(), object)
			if err != nil && forceUpdate {
				var errJson error
				existedObjectJsonString, errJson := json.MarshalIndent(emptyObject, "", "  ")
				if errJson != nil {
					existedObjectJsonString = []byte("Not able to parse object. Error: " + errJson.Error())
				}
				newJsonString, errJson := json.MarshalIndent(object, "", "  ")
				if errJson != nil {
					newJsonString = []byte("Not able to parse object. Error: " + errJson.Error())
				}
				return &ExecutionError{
					Msg: fmt.Sprintf(
						"Resource update is failed with the following message: %s\nExisted resource: %s\nNew resource: %s\n",
						err.Error(),
						string(existedObjectJsonString),
						string(newJsonString)),
				}
			}
		}
	}

	return nil
}

func Zero(x interface{}) interface{} {
	elemValue := reflect.ValueOf(x)
	if elemValue.Kind() == reflect.Ptr {
		elemValue = reflect.ValueOf(elemValue.Elem().Interface())
	}
	res := reflect.Zero(elemValue.Type()).Interface()
	val := reflect.ValueOf(res)
	p := reflect.New(reflect.TypeOf(val.Interface()))
	p.Elem().Set(val)

	return p.Interface()
}

func ContainsInt(arr []int, str int) bool {
	for _, a := range arr {
		if a == str {
			return true
		}
	}
	return false
}

func ContainsStr(arr [3]string, str string) bool {
	for _, a := range arr {
		if a == str {
			return true
		}
	}
	return false
}

func ConcatMaps(additionalNodes map[string]string, str map[string]string) map[string]string {

	if additionalNodes == nil && str == nil {
		return nil
	}

	if additionalNodes == nil {
		return str
	}

	if str == nil {
		return additionalNodes
	}

	for k, v := range additionalNodes {
		str[k] = v
	}

	return str
}

func OptionalString(src string, defaultStr string) string {
	if src == "" {
		return defaultStr
	}

	return src
}

func OptionalStringToInt(str string, defaultInt int) int {
	if str != "" {
		if ivalue, err := strconv.Atoi(str); err == nil {
			return ivalue
		}
	}
	return defaultInt
}

func GetLogger(debug bool) *zap.Logger {
	var atom zap.AtomicLevel
	if debug {
		atom = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	} else {
		atom = zap.NewAtomicLevel()
	}
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer logger.Sync()
	return logger
}

func AddServiceDeployResultToContext(ctx ExecutionContext, serviceName string, result string) {
	iInfo := ctx.Get(constants.ContextServiceDeploymentInfo)

	info := map[string]string{}
	if iInfo != nil {
		info = iInfo.(map[string]string)
	}

	info[serviceName] = result
	ctx.Set(constants.ContextServiceDeploymentInfo, info)
}

func GetMicroServiceDeployType(ctx ExecutionContext, serviceName string) MicroServiceDeployType {
	iInfo := ctx.Get(constants.ContextServiceDeploymentInfo)

	if iInfo == nil {
		return CleanDeploy
	} else {
		info := iInfo.(map[string]string)

		serviceStatus := info[serviceName]

		if serviceStatus == "" {
			return CleanDeploy
		} else if serviceStatus == constants.MicroServiceSuccessDeploymentResult {
			return Update
		} else {
			//return CleanupRequired
			return CleanDeploy
		}
	}
}

func GetCurrentDeployType(ctx ExecutionContext) MicroServiceDeployType {
	iCurrent := ctx.Get(constants.ContextServiceDeployType)

	current := Empty
	if iCurrent != nil {
		current = iCurrent.(MicroServiceDeployType)
	} else {
		SetCurrentDeployType(ctx, current)
	}
	return current
}

func SetCurrentDeployType(ctx ExecutionContext, deployType MicroServiceDeployType) {
	ctx.Set(constants.ContextServiceDeployType, deployType)
}

func HandleError(err error, log func(msg string, fields ...zap.Field), message string) {
	if err != nil {
		log(message)
	}
}

func PanicError(err error, log func(msg string, fields ...zap.Field), message string) {
	HandleError(err, log, message)
	if err != nil {
		panic(&ExecutionError{Msg: fmt.Sprintf("%s\n%s", message, err.Error())})
	}
}

// Allows receive map of param and value by tag set on the field in struct
// for example
//
//	type Test struct {
//	    Foo   string `json:"foo"`
//	    Bar   int    `json: bar"`
//	}
//
// GetFieldsAndNamesByTag(fieldName, "foo", "json", &Test{"123", 456})
// fieldname will be filled with {"Foo":"123"}
//
// works with embedded structs and structs by pointer
func GetFieldsAndNamesByTag(fieldName map[string]interface{}, tag, key string, s interface{}, depth *int) {
	rt := reflect.TypeOf(s)
	rv := reflect.ValueOf(s)
	if rt.Kind() != reflect.Struct {
		panic("bad type")
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		val := rv.Field(i)
		v := strings.Split(f.Tag.Get(key), ",")[0] // use split to ignore tag "options"
		if v == tag {
			fieldName[f.Name] = val.Interface()
		}
		if *depth > 0 {
			*depth--
			if val.Kind() == reflect.Struct {
				GetFieldsAndNamesByTag(fieldName, tag, key, val.Interface(), depth)

			} else if val.Kind() == reflect.Ptr && val.Elem().Kind() == reflect.Struct {
				GetFieldsAndNamesByTag(fieldName, tag, key, val.Elem().Interface(), depth)
			}
			*depth++
		}
	}
}

func ReadFromFile(filePath string) (string, error) {
	dat, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}
