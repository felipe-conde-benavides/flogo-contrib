package instance

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/TIBCOSoftware/flogo-contrib/action/flow/definition"
	"github.com/TIBCOSoftware/flogo-contrib/action/flow/model"
	"github.com/TIBCOSoftware/flogo-lib/core/activity"
	"github.com/TIBCOSoftware/flogo-lib/core/data"
	"github.com/TIBCOSoftware/flogo-lib/core/mapper/exprmapper"
	"github.com/TIBCOSoftware/flogo-lib/logger"
	"github.com/TIBCOSoftware/flogo-contrib/action/flow/event"
	corevent "github.com/TIBCOSoftware/flogo-lib/core/event"
	"github.com/TIBCOSoftware/flogo-lib/app"
	"time"
)

func NewTaskInst(inst *Instance, task *definition.Task) *TaskInst {
	var taskInst TaskInst

	taskInst.flowInst = inst
	taskInst.task = task
	taskInst.taskID = task.ID()
	return &taskInst
}

type TaskInst struct {
	flowInst *Instance
	task     *definition.Task
	status   model.TaskStatus

	workingData map[string]*data.Attribute

	inScope  data.Scope
	outScope data.Scope

	returnError error

	taskID string //needed for serialization
}

//DEPRECATED
func (ti *TaskInst) FlowDetails() activity.FlowDetails {
	return ti.flowInst
}

// InputScope get the InputScope of the task instance
func (ti *TaskInst) InputScope() data.Scope {

	if ti.inScope != nil {
		return ti.inScope
	}

	if len(ti.task.ActivityConfig().Ref()) > 0 {

		act := activity.Get(ti.task.ActivityConfig().Ref())
		if act.Metadata().DynamicIO {

			//todo validate dynamic on instantiation
			dynamic, _ := act.(activity.DynamicIO)
			dynamicIO, err := dynamic.IOMetadata(ti)

			if err == nil {
				ti.inScope = NewFixedTaskScope(dynamicIO.Input, ti.task, true)
			} else {
				//todo handle err
				ti.inScope = NewFixedTaskScope(act.Metadata().Input, ti.task, true)
			}
		} else {
			ti.inScope = NewFixedTaskScope(act.Metadata().Input, ti.task, true)
		}

	} else if ti.task.IsScope() {

		//add flow scope
	}

	return ti.inScope
}

// OutputScope get the InputScope of the task instance
func (ti *TaskInst) OutputScope() data.Scope {

	if ti.outScope != nil {
		return ti.outScope
	}

	if len(ti.task.ActivityConfig().Ref()) > 0 {

		act := activity.Get(ti.task.ActivityConfig().Ref())

		outputMetadta := act.Metadata().Output

		if act.Metadata().DynamicIO {
			//todo validate dynamic on instantiation
			dynamic, _ := act.(activity.DynamicIO)
			dynamicIO, _ := dynamic.IOMetadata(ti)
			//todo handler error
			if dynamicIO != nil {
				outputMetadta = dynamicIO.Output
			}
		}

		ti.outScope = NewFixedTaskScope(outputMetadta, ti.task, false)

		//logger.Debugf("OutputScope: %#v", ti.outScope)
	} else if ti.task.IsScope() {

		//add flow scope
	}

	return ti.outScope
}

/////////////////////////////////////////
// TaskInst - activity.Context Implementation

func (ti *TaskInst) ActivityHost() activity.Host {
	return ti.flowInst
}

// Name implements activity.Context.Name method
func (ti *TaskInst) Name() string {
	return ti.task.Name()
}

// GetSetting implements activity.Context.GetSetting
func (ti *TaskInst) GetSetting(setting string) (value interface{}, exists bool) {

	val, found := ti.Task().ActivityConfig().GetSetting(setting)
	if found {
		return val.Value(), true
	}

	return nil, false
}

// GetInitValue implements activity.Context.GetInitValue
func (ti *TaskInst) GetInitValue(key string) (value interface{}, exists bool) {
	return nil, false
}

// GetInput implements activity.Context.GetInput
func (ti *TaskInst) GetInput(name string) interface{} {

	val, found := ti.InputScope().GetAttr(name)
	if found {
		return val.Value()
	}

	return nil
}

// GetOutput implements activity.Context.GetOutput
func (ti *TaskInst) GetOutput(name string) interface{} {

	val, found := ti.OutputScope().GetAttr(name)
	if found {
		return val.Value()
	}

	return nil
}

// SetOutput implements activity.Context.SetOutput
func (ti *TaskInst) SetOutput(name string, value interface{}) {

	logger.Debugf("SET OUTPUT: %s = %v\n", name, value)
	ti.OutputScope().SetAttrValue(name, value)
}

func (ti *TaskInst) GetSharedTempData() map[string]interface{} {

	//todo implement
	return nil
}

// TaskName implements activity.Context.TaskName method
// Deprecated
func (ti *TaskInst) TaskName() string {
	return ti.task.Name()
}

/////////////////////////////////////////
// TaskInst - TaskContext Implementation

// Status implements flow.TaskContext.GetState
func (ti *TaskInst) Status() model.TaskStatus {
	return ti.status
}

// SetStatus implements flow.TaskContext.SetStatus
func (ti *TaskInst) SetStatus(status model.TaskStatus) {
	ti.status = status
	ti.flowInst.master.ChangeTracker.trackTaskData(ti.flowInst.subFlowId, &TaskInstChange{ChgType: CtUpd, ID: ti.task.ID(), TaskInst: ti})

	// publish event
	postTaskEvent(ti)
}

func (ti *TaskInst) HasWorkingData() bool {
	return ti.workingData != nil
}

func (ti *TaskInst) Resolve(toResolve string) (value interface{}, err error) {
	//Support expression mapping
	return exprmapper.GetExpresssionValue(toResolve, ti.flowInst, definition.GetDataResolver())
}

func (ti *TaskInst) AddWorkingData(attr *data.Attribute) {

	if ti.workingData == nil {
		ti.workingData = make(map[string]*data.Attribute)
	}
	ti.workingData[attr.Name()] = attr
}

func (ti *TaskInst) UpdateWorkingData(key string, value interface{}) error {

	if ti.workingData == nil {
		return errors.New("working data '" + key + "' not defined")
	}

	attr, ok := ti.workingData[key]

	if ok {
		attr.SetValue(value)
	} else {
		return errors.New("working data '" + key + "' not defined")
	}

	return nil
}

func (ti *TaskInst) GetWorkingData(key string) (*data.Attribute, bool) {
	if ti.workingData == nil {
		return nil, false
	}

	v, ok := ti.workingData[key]
	return v, ok
}

// Task implements model.TaskContext.Task, by returning the Task associated with this
// TaskInst object
func (ti *TaskInst) Task() *definition.Task {
	return ti.task
}

// GetFromLinkInstances implements model.TaskContext.GetFromLinkInstances
func (ti *TaskInst) GetFromLinkInstances() []model.LinkInstance {

	//logger.Debugf("GetFromLinkInstances: task=%v", ti.Task)

	links := ti.task.FromLinks()

	numLinks := len(links)

	if numLinks > 0 {
		linkCtxs := make([]model.LinkInstance, numLinks)

		for i, link := range links {
			linkCtxs[i], _ = ti.flowInst.FindOrCreateLinkData(link)
		}
		return linkCtxs
	}

	return nil
}

// GetToLinkInstances implements model.TaskContext.GetToLinkInstances,
func (ti *TaskInst) GetToLinkInstances() []model.LinkInstance {

	//logger.Debugf("GetToLinkInstances: task=%v\n", ti.Task)

	links := ti.task.ToLinks()

	numLinks := len(links)

	if numLinks > 0 {
		linkCtxs := make([]model.LinkInstance, numLinks)

		for i, link := range links {
			linkCtxs[i], _ = ti.flowInst.FindOrCreateLinkData(link)
		}
		return linkCtxs
	}

	return nil
}

// EvalLink implements activity.ActivityContext.EvalLink method
func (ti *TaskInst) EvalLink(link *definition.Link) (result bool, err error) {

	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("Unhandled Error evaluating link '%s' : %v\n", link.ID(), r)

			// todo: useful for debugging
			logger.Debugf("StackTrace: %s", debug.Stack())

			if err != nil {
				err = fmt.Errorf("%v", r)
			}
		}
	}()

	mgr := ti.flowInst.flowDef.GetLinkExprManager()

	if mgr != nil {
		result, err = mgr.EvalLinkExpr(link, ti.flowInst)
		return result, err
	}

	return true, nil
}

// HasActivity implements activity.ActivityContext.HasActivity method
func (ti *TaskInst) HasActivity() bool {
	return activity.Get(ti.task.ActivityConfig().Ref()) != nil
}

// EvalActivity implements activity.ActivityContext.EvalActivity method
func (ti *TaskInst) EvalActivity() (done bool, evalErr error) {

	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("Unhandled Error executing activity '%s'[%s] : %v\n", ti.task.Name(), ti.task.ActivityConfig().Ref(), r)

			// todo: useful for debugging
			logger.Debugf("StackTrace: %s", debug.Stack())

			if evalErr == nil {
				evalErr = NewActivityEvalError(ti.task.Name(), "unhandled", fmt.Sprintf("%v", r))
				done = false
			}
		}
		if evalErr != nil {
			logger.Errorf("Execution failed for Activity[%s] in Flow[%s] - %s", ti.task.Name(), ti.flowInst.flowDef.Name(), evalErr.Error())
		}
	}()

	eval := true

	if ti.task.ActivityConfig().InputMapper() != nil {

		err := applyInputMapper(ti)

		if err != nil {

			evalErr = NewActivityEvalError(ti.task.Name(), "mapper", err.Error())
			return false, evalErr
		}
	}

	//if taskData.HasAttrs() {
	eval = applyInputInterceptor(ti)

	if eval {

		act := activity.Get(ti.task.ActivityConfig().Ref())
		done, evalErr = act.Eval(ti)

		if evalErr != nil {
			e, ok := evalErr.(*activity.Error)
			if ok {
				e.SetActivityName(ti.task.Name())
			}

			return false, evalErr
		}
	} else {
		done = true
	}

	if done {

		//if taskData.HasAttrs() {
		applyOutputInterceptor(ti)

		if ti.task.ActivityConfig().OutputMapper() != nil {

			appliedMapper, err := applyOutputMapper(ti)

			if err != nil {
				evalErr = NewActivityEvalError(ti.task.Name(), "mapper", err.Error())
				return done, evalErr
			}

			if !appliedMapper && !ti.task.IsScope() {

				logger.Debug("Mapper not applied")
			}
		}
	}

	return done, nil
}

// EvalActivity implements activity.ActivityContext.EvalActivity method
func (ti *TaskInst) PostEvalActivity() (done bool, evalErr error) {

	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("Unhandled Error executing activity '%s'[%s] : %v\n", ti.task.Name(), ti.task.ActivityConfig().Ref(), r)

			// todo: useful for debugging
			logger.Debugf("StackTrace: %s", debug.Stack())

			if evalErr == nil {
				evalErr = NewActivityEvalError(ti.task.Name(), "unhandled", fmt.Sprintf("%v", r))
				done = false
			}
		}
		if evalErr != nil {
			logger.Errorf("Execution failed for Activity[%s] in Flow[%s] - %s", ti.task.Name(), ti.flowInst.flowDef.Name(), evalErr.Error())
		}
	}()

	act := activity.Get(ti.task.ActivityConfig().Ref())

	aa, ok := act.(activity.AsyncActivity)
	done = true

	if ok {
		done, evalErr = aa.PostEval(ti, nil)

		if evalErr != nil {
			e, ok := evalErr.(*activity.Error)
			if ok {
				e.SetActivityName(ti.task.Name())
			}

			return false, evalErr
		}
	}

	if done {

		if ti.task.ActivityConfig().OutputMapper() != nil {
			applyOutputInterceptor(ti)

			appliedMapper, err := applyOutputMapper(ti)

			if err != nil {
				evalErr = NewActivityEvalError(ti.task.Name(), "mapper", err.Error())
				return done, evalErr
			}

			if !appliedMapper && !ti.task.IsScope() {

				logger.Debug("Mapper not applied")
			}
		}
	}

	return done, nil
}

// FlowReply is used to reply to the Flow Host with the results of the execution
func (ti *TaskInst) FlowReply(replyData map[string]*data.Attribute, err error) {
	//ignore
}

// FlowReturn is used to indicate to the Flow Host that it should complete and return the results of the execution
func (ti *TaskInst) FlowReturn(returnData map[string]*data.Attribute, err error) {

	if err != nil {
		for _, value := range returnData {
			ti.AddWorkingData(value)
		}
	}
}

func (taskInst *TaskInst) appendErrorData(err error) {

	switch e := err.(type) {
	case *definition.LinkExprError:
		taskInst.flowInst.AddAttr("_E.type", data.TypeString, "link_expr")
		taskInst.flowInst.AddAttr("_E.message", data.TypeString, err.Error())
		taskInst.flowInst.AddAttr("_E.data", data.TypeObject, nil)
		taskInst.flowInst.AddAttr("_E.code", data.TypeString, "")
		taskInst.flowInst.AddAttr("_E.activity", data.TypeString, taskInst.taskID)
	case *activity.Error:
		taskInst.flowInst.AddAttr("_E.type", data.TypeString, "activity")
		taskInst.flowInst.AddAttr("_E.message", data.TypeString, err.Error())
		taskInst.flowInst.AddAttr("_E.data", data.TypeObject, e.Data())
		taskInst.flowInst.AddAttr("_E.code", data.TypeString, e.Code())

		if e.ActivityName() != "" {
			taskInst.flowInst.AddAttr("_E.activity", data.TypeString, e.ActivityName())
		} else {
			taskInst.flowInst.AddAttr("_E.activity", data.TypeString, taskInst.taskID)
		}
	case *ActivityEvalError:
		taskInst.flowInst.AddAttr("_E.activity", data.TypeString, e.TaskName())
		taskInst.flowInst.AddAttr("_E.message", data.TypeString, err.Error())
		taskInst.flowInst.AddAttr("_E.type", data.TypeString, e.Type())
		taskInst.flowInst.AddAttr("_E.data", data.TypeObject, nil)
		taskInst.flowInst.AddAttr("_E.code", data.TypeString, "")
	default:
		taskInst.flowInst.AddAttr("_E.activity", data.TypeString, taskInst.taskID)
		taskInst.flowInst.AddAttr("_E.message", data.TypeString, err.Error())
		taskInst.flowInst.AddAttr("_E.type", data.TypeString, "unknown")
		taskInst.flowInst.AddAttr("_E.data", data.TypeObject, nil)
		taskInst.flowInst.AddAttr("_E.code", data.TypeString, "")
	}

	//todo add case for *dataMapperError & *activity.Error
}

//// Failed marks the Activity as failed
//func (td *TaskInst) Failed(err error) {
//
//	errorMsgAttr := "[A" + td.task.ID() + "._errorMsg]"
//	td.inst.AddAttr(errorMsgAttr, data.STRING, err.Error())
//	errorMsgAttr2 := "[activity." + td.task.ID() + "._errorMsg]"
//	td.inst.AddAttr(errorMsgAttr2, data.STRING, err.Error())
//}

// FlowDetails implements activity.Context.FlowName method
//func (ti *TaskInst) FlowDetails() activity.FlowDetails {
//	return ti.flowInst
//}
//

type taskEvent struct {
	time                           time.Time
	err                            error
	taskIn, taskOut                map[string]interface{}
	status                         event.Status
	name, typeId, flowName, flowId string
}

// Returns flow name
func (te *taskEvent) FlowName() string {
	return te.flowName
}

// Returns flow ID
func (te *taskEvent) FlowID() string {
	return te.flowId
}

// Returns task name
func (te *taskEvent) Name() string {
	return te.name
}

// Returns task type
func (te *taskEvent) Type() string {
	return te.typeId
}

// Returns task status
func (te *taskEvent) Status() event.Status {
	return te.status
}

// Returns application name
func (te *taskEvent) AppName() string {
	return app.GetName()
}

// Returns application version
func (te *taskEvent) AppVersion() string {
	return app.GetVersion()
}

// Returns event time
func (te *taskEvent) Time() time.Time {
	return te.time
}

// Returns activity input data
func (te *taskEvent) TaskInput() map[string]interface{} {
	return te.taskIn
}

// Returns output data for completed activity
func (te *taskEvent) TaskOutput() map[string]interface{} {
	return te.taskOut
}

// Returns error for failed task
func (te *taskEvent) Error() error {
	return te.err
}

func convertTaskStatus(code model.TaskStatus) event.Status {
	switch code {
	case model.TaskStatusNotStarted:
		return event.CREATED
	case model.TaskStatusEntered:
		return event.SCHEDULED
	case model.TaskStatusSkipped:
		return event.SKIPPED
	case model.TaskStatusReady:
		return event.STARTED
	case model.TaskStatusFailed:
		return event.FAILED
	case model.TaskStatusDone:
		return event.COMPLETED
	case model.TaskStatusWaiting:
		return event.WAITING
	}
	return event.UNKNOWN
}

func postTaskEvent(taskInstance *TaskInst) {
	if corevent.HasListener(event.TASK_EVENT_TYPE) {
		te := &taskEvent{}
		te.time = time.Now()
		te.name = taskInstance.Task().Name()
		te.status = convertTaskStatus(taskInstance.Status())
		te.flowName = taskInstance.flowInst.Name()
		te.flowId = taskInstance.flowInst.ID()
		te.typeId = taskInstance.Task().TypeID()

		if te.status == event.FAILED {
			te.err = taskInstance.returnError
		}

		te.taskIn = make(map[string]interface{})
		te.taskOut = make(map[string]interface{})

		// Add working data
		wData := taskInstance.workingData
		if wData != nil && len(wData) > 0 {
			for name, attVal := range wData {
				te.taskIn[name] = attVal.Value()
			}
		}

		// Add activity input
		if taskInstance.HasActivity() {
			actConfig := taskInstance.Task().ActivityConfig()
			if actConfig != nil && actConfig.Activity != nil && actConfig.Activity.Metadata() != nil {
				metadata := actConfig.Activity.Metadata()
				if metadata.Input != nil && len(metadata.Input) > 0 && taskInstance.InputScope() != nil {
					for name, attVal := range actConfig.Activity.Metadata().Input {
						scopedValue, ok := taskInstance.InputScope().GetAttr(name)
						if !ok {
							te.taskIn[name] = attVal.Value()
						} else {
							te.taskIn[name] = scopedValue.Value()
						}
					}
				}

				if te.status == event.COMPLETED && metadata.Output != nil && len(metadata.Output) > 0 && taskInstance.OutputScope() != nil {
					for name, attVal := range actConfig.Activity.Metadata().Output {
						scopedValue, ok := taskInstance.OutputScope().GetAttr(name)
						if !ok {
							te.taskOut[name] = attVal.Value()
						} else {
							te.taskOut[name] = scopedValue.Value()
						}
					}
				}

				if metadata.DynamicIO {
					// Get dynamic input/outputs
					dynamic, _ := actConfig.Activity.(activity.DynamicIO)
					dynamicIO, _ := dynamic.IOMetadata(taskInstance)
					if dynamicIO != nil {
						if dynamicIO.Input != nil {
							for name, attVal := range dynamicIO.Input {
								scopedValue, ok := taskInstance.InputScope().GetAttr(name)
								if !ok {
									te.taskIn[name] = attVal.Value()
								} else {
									te.taskIn[name] = scopedValue.Value()
								}
							}
						}

						if te.status == event.COMPLETED && dynamicIO.Output != nil {
							for name, attVal := range dynamicIO.Input {
								scopedValue, ok := taskInstance.OutputScope().GetAttr(name)
								if !ok {
									te.taskOut[name] = attVal.Value()
								} else {
									te.taskOut[name] = scopedValue.Value()
								}
							}
						}
					}
				}
			}
		}
		corevent.PostEvent(event.TASK_EVENT_TYPE, te)
	}

}
