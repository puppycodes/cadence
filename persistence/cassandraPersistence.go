package persistence

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/pborman/uuid"

	workflow "code.uber.internal/devexp/minions/.gen/go/shared"
	"code.uber.internal/devexp/minions/common"
)

const (
	cassandraProtoVersion   = 4
	defaultSessionTimeout   = 10 * time.Second
	rowTypeExecutionTaskID  = int64(77)
	permanentRunID          = "dcb940ac-0c63-ffa2-ffea-a6c305881d71"
	rowTypeShardWorkflowID  = "3fe89dad-8326-fac5-fd40-fe08cfa25dec"
	rowTypeShardRunID       = "228ce20b-af54-fe2f-ff17-be728a00f785"
	rowTypeShardTaskID      = int64(23)
	defaultDeleteTTLSeconds = int64(time.Hour*24*7) / int64(time.Second) // keep deleted records for 7 days
)

// Row types for table executions
const (
	rowTypeShard = iota
	rowTypeExecution
	rowTypeTransferTask
	rowTypeTimerTask
)

const (
	templateShardType = `{` +
		`shard_id: ?, ` +
		`range_id: ?, ` +
		`transfer_ack_level: ?` +
		`}`

	templateWorkflowExecutionType = `{` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`task_list: ?, ` +
		`history: ?, ` +
		`execution_context: ?, ` +
		`state: ?, ` +
		`next_event_id: ?, ` +
		`last_processed_event: ?, ` +
		`last_updated_time: ?, ` +
		`decision_pending: ?` +
		`}`

	templateTaskType = `{` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`task_id: ?, ` +
		`task_list: ?, ` +
		`type: ?, ` +
		`schedule_id: ?, ` +
		`visibility_time: ?, ` +
		`lock_token: ?, ` +
		`delivery_count: ?` +
		`}`

	templateCreateShardQuery = `INSERT INTO executions (` +
		`shard_id, type, workflow_id, run_id, task_id, shard, range_id)` +
		`VALUES(?, ?, ?, ?, ?, ` + templateShardType + `, ?) IF NOT EXISTS`

	templateGetShardQuery = `SELECT shard ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ?`

	templateUpdateShardQuery = `UPDATE executions ` +
		`SET shard = ` + templateShardType + `, range_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ? ` +
		`IF range_id = ?`

	templateCreateWorkflowExecutionQuery = `INSERT INTO executions (` +
		`shard_id, type, workflow_id, run_id, task_id, current_run_id) ` +
		`VALUES(?, ?, ?, ?, ?, ?) IF NOT EXISTS`

	templateCreateWorkflowExecutionQuery2 = `INSERT INTO executions (` +
		`shard_id, workflow_id, run_id, type, execution, next_event_id, task_id) ` +
		`VALUES(?, ?, ?, ?, ` + templateWorkflowExecutionType + `, ?, ?) IF NOT EXISTS`

	templateCreateTransferTaskQuery = `INSERT INTO executions (` +
		`shard_id, type, workflow_id, run_id, transfer, task_id, lock_token) ` +
		`VALUES(?, ?, ?, ?, ` + templateTaskType + `, ?, ?)`

	templateUpdateLeaseQuery = `UPDATE executions ` +
		`SET range_id = ? ` +
		`WHERE shard_id = ? ` +
		`IF range_id = ?`

	templateGetWorkflowExecutionQuery = `SELECT execution ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ?`

	templateUpdateWorkflowExecutionQuery = `UPDATE executions ` +
		`SET execution = ` + templateWorkflowExecutionType + `, next_event_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ? and range_id = ?`

	templateDeleteWorkflowExecutionQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ? `

	templateDeleteWorkflowExecutionTTLQuery = `INSERT INTO executions (` +
		`shard_id, workflow_id, run_id, type, execution, next_event_id, task_id) ` +
		`VALUES(?, ?, ?, ?, ` + templateWorkflowExecutionType + `, ?, ?) USING TTL ?`

	templateGetTransferTasksQuery = `SELECT transfer ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ?`

	templateLockTransferTaskQuery = `UPDATE executions ` +
		`SET transfer = ` + templateTaskType + `, lock_token = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ? ` +
		`IF lock_token = ?`

	templateCompleteTransferTaskQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ?` +
		`IF lock_token = ?`

	templateCreateTaskQuery = `INSERT INTO tasks (` +
		`task_list, type, workflow_id, run_id, task_id, task, lock_token) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateTaskType + `, ?)`

	templateGetTasksQuery = `SELECT task_id, task ` +
		`FROM tasks ` +
		`WHERE task_list = ? ` +
		`and type = ? `

	templateLockTaskQuery = `UPDATE tasks ` +
		`SET task = ` + templateTaskType + `, lock_token = ? ` +
		`WHERE task_list = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ? ` +
		`IF lock_token = ?`

	templateCompleteTaskQuery = `DELETE FROM tasks ` +
		`WHERE task_list = ? ` +
		`and type = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and task_id = ?` +
		`IF lock_token = ?`
)

type (
	cassandraPersistence struct {
		session      *gocql.Session
		lowConslevel gocql.Consistency
		shardID      int
	}
)

// NewCassandraWorkflowExecutionPersistence is used to create an instance of workflowExecutionManager implementation
func NewCassandraWorkflowExecutionPersistence(hosts string, keyspace string, shardID int) (ExecutionManager, error) {
	cluster := common.NewCassandraCluster(hosts)
	cluster.Keyspace = keyspace
	cluster.ProtoVersion = cassandraProtoVersion
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = defaultSessionTimeout

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}
	return &cassandraPersistence{shardID: shardID, session: session, lowConslevel: gocql.One}, nil
}

// NewCassandraTaskPersistence is used to create an instance of TaskManager implementation
func NewCassandraTaskPersistence(hosts string, keyspace string) (TaskManager, error) {
	cluster := common.NewCassandraCluster(hosts)
	cluster.Keyspace = keyspace
	cluster.ProtoVersion = cassandraProtoVersion
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = defaultSessionTimeout

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}
	return &cassandraPersistence{shardID: -1, session: session, lowConslevel: gocql.One}, nil
}

func (d *cassandraPersistence) CreateShard(request *CreateShardRequest) error {
	shardInfo := request.ShardInfo
	query := d.session.Query(templateCreateShardQuery,
		shardInfo.ShardID,
		rowTypeShard,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		rowTypeShardTaskID,
		shardInfo.ShardID,
		shardInfo.RangeID,
		shardInfo.TransferAckLevel,
		shardInfo.RangeID)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CreateShard operation failed. Error : %v", err),
		}
	}

	if !applied {
		shard := previous["shard"].(map[string]interface{})
		return &ShardAlreadyExistError{
			msg: fmt.Sprintf("Shard already exists in executions table.  ShardId: %v, RangeId: %v",
				shard["shard_id"], shard["range_id"]),
		}
	}

	return nil
}

func (d *cassandraPersistence) GetShard(request *GetShardRequest) (*GetShardResponse, error) {
	shardID := request.ShardID
	query := d.session.Query(templateGetShardQuery,
		shardID,
		rowTypeShard,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		rowTypeShardTaskID).Consistency(d.lowConslevel)

	result := make(map[string]interface{})
	if err := query.MapScan(result); err != nil {
		if err == gocql.ErrNotFound {
			return nil, &workflow.EntityNotExistsError{
				Message: fmt.Sprintf("Shard not found.  ShardId: %v", shardID),
			}
		}

		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetShard operation failed. Error: %v", err),
		}
	}

	info := createShardInfo(result["shard"].(map[string]interface{}))
	return &GetShardResponse{ShardInfo: info}, nil
}

func (d *cassandraPersistence) UpdateShard(request *UpdateShardRequest) error {
	shardInfo := request.ShardInfo

	query := d.session.Query(templateUpdateShardQuery,
		shardInfo.ShardID,
		shardInfo.RangeID,
		shardInfo.TransferAckLevel,
		shardInfo.RangeID,
		shardInfo.ShardID,
		rowTypeShard,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		rowTypeShardTaskID,
		request.PreviousRangeID)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("UpdateShard operation failed. Error: %v", err),
		}
	}

	if !applied {
		var columns []string
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%s=%v", k, v))
		}

		return &ConditionFailedError{
			msg: fmt.Sprintf("Failed to update shard.  previous_range_id: %v, columns: (%v)",
				request.PreviousRangeID, strings.Join(columns, ",")),
		}
	}

	return nil
}

func (d *cassandraPersistence) CreateWorkflowExecution(request *CreateWorkflowExecutionRequest) (
	*CreateWorkflowExecutionResponse, error) {
	cqlNowTimestamp := common.UnixNanoToCQLTimestamp(time.Now().UnixNano())
	transferTaskID := uuid.New()

	batch := d.session.NewBatch(gocql.LoggedBatch)
	batch.Query(templateCreateWorkflowExecutionQuery,
		d.shardID,
		rowTypeExecution,
		request.Execution.GetWorkflowId(),
		permanentRunID,
		rowTypeExecutionTaskID,
		request.Execution.GetRunId())

	batch.Query(templateCreateWorkflowExecutionQuery2,
		d.shardID,
		request.Execution.GetWorkflowId(),
		request.Execution.GetRunId(),
		rowTypeExecution,
		request.Execution.GetWorkflowId(),
		request.Execution.GetRunId(),
		request.TaskList,
		request.History,
		request.ExecutionContext,
		WorkflowStateCreated,
		request.NextEventID,
		request.LastProcessedEvent,
		cqlNowTimestamp,
		true,
		request.NextEventID,
		rowTypeExecutionTaskID)

	d.createTransferTasks(batch, request.TransferTasks, request.Execution.GetWorkflowId(), request.Execution.GetRunId(),
		cqlNowTimestamp)

	batch.Query(templateUpdateLeaseQuery,
		request.RangeID,
		d.shardID,
		request.RangeID,
	)

	previous := make(map[string]interface{})
	applied, _, err := d.session.MapExecuteBatchCAS(batch, previous)
	if err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("CreateWorkflowExecution operation failed. Error: %v", err),
		}
	}

	if !applied {
		var columns []string
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%s=%v", k, v))
		}

		execution := previous["execution"].(map[string]interface{})
		return nil, &workflow.WorkflowExecutionAlreadyStartedError{
			Message: fmt.Sprintf("Workflow execution already running. WorkflowId: %v, RunId: %v, rangeID: %v, columns: (%v)",
				execution["workflow_id"], execution["run_id"], request.RangeID, strings.Join(columns, ",")),
		}
	}

	return &CreateWorkflowExecutionResponse{TaskID: transferTaskID}, nil
}

func (d *cassandraPersistence) GetWorkflowExecution(request *GetWorkflowExecutionRequest) (
	*GetWorkflowExecutionResponse, error) {
	execution := request.Execution
	query := d.session.Query(templateGetWorkflowExecutionQuery,
		d.shardID,
		rowTypeExecution,
		execution.GetWorkflowId(),
		execution.GetRunId(),
		rowTypeExecutionTaskID).Consistency(d.lowConslevel)

	result := make(map[string]interface{})
	if err := query.MapScan(result); err != nil {
		if err == gocql.ErrNotFound {
			return nil, &workflow.EntityNotExistsError{
				Message: fmt.Sprintf("Workflow execution not found.  WorkflowId: %v, RunId: %v", execution.GetWorkflowId(),
					execution.GetRunId()),
			}
		}

		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetWorkflowExecution operation failed. Error: %v", err),
		}
	}

	info := createWorkflowExecutionInfo(result["execution"].(map[string]interface{}))

	return &GetWorkflowExecutionResponse{ExecutionInfo: info}, nil
}

func (d *cassandraPersistence) UpdateWorkflowExecution(request *UpdateWorkflowExecutionRequest) error {
	executionInfo := request.ExecutionInfo
	cqlNowTimestamp := common.UnixNanoToCQLTimestamp(time.Now().UnixNano())

	batch := d.session.NewBatch(gocql.LoggedBatch)
	batch.Query(templateUpdateWorkflowExecutionQuery,
		executionInfo.WorkflowID,
		executionInfo.RunID,
		executionInfo.TaskList,
		executionInfo.History,
		executionInfo.ExecutionContext,
		executionInfo.State,
		executionInfo.NextEventID,
		executionInfo.LastProcessedEvent,
		cqlNowTimestamp,
		executionInfo.DecisionPending,
		executionInfo.NextEventID,
		d.shardID,
		rowTypeExecution,
		executionInfo.WorkflowID,
		executionInfo.RunID,
		rowTypeExecutionTaskID,
		request.Condition,
		request.RangeID)

	d.createTransferTasks(batch, request.TransferTasks, executionInfo.WorkflowID, executionInfo.RunID, cqlNowTimestamp)

	previous := make(map[string]interface{})
	applied, _, err := d.session.MapExecuteBatchCAS(batch, previous)
	if err != nil {
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("UpdateWorkflowExecution operation failed. Error: %v", err),
		}
	}

	if !applied {
		var columns []string
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%s=%v", k, v))
		}

		return &ConditionFailedError{
			msg: fmt.Sprintf("Failed to update workflow execution.  condition: %v, columns: (%v)",
				request.Condition, strings.Join(columns, ",")),
		}
	}

	return nil
}

func (d *cassandraPersistence) DeleteWorkflowExecution(request *DeleteWorkflowExecutionRequest) error {
	info := request.ExecutionInfo
	cqlNowTimestamp := common.UnixNanoToCQLTimestamp(time.Now().UnixNano())
	batch := d.session.NewBatch(gocql.LoggedBatch)
	batch.Query(templateDeleteWorkflowExecutionQuery,
		d.shardID,
		rowTypeExecution,
		info.WorkflowID,
		permanentRunID,
		rowTypeExecutionTaskID)

	batch.Query(templateDeleteWorkflowExecutionTTLQuery,
		d.shardID,
		info.WorkflowID,
		info.RunID,
		rowTypeExecution,
		info.WorkflowID,
		info.RunID,
		info.TaskList,
		info.History,
		info.ExecutionContext,
		info.State,
		info.NextEventID,
		info.LastProcessedEvent,
		cqlNowTimestamp,
		info.DecisionPending,
		info.NextEventID,
		rowTypeExecutionTaskID,
		defaultDeleteTTLSeconds)

	err := d.session.ExecuteBatch(batch)
	if err != nil {
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("DeleteWorkflowExecution operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) GetTransferTasks(request *GetTransferTasksRequest) (*GetTransferTasksResponse, error) {
	currentTimestamp := time.Now()

	query := d.session.Query(templateGetTransferTasksQuery,
		d.shardID,
		rowTypeTransferTask).Consistency(d.lowConslevel)

	iter := query.Iter()
	if iter == nil {
		return nil, &workflow.InternalServiceError{
			Message: "GetTransferTasks operation failed.  Not able to create query iterator.",
		}
	}

	response := &GetTransferTasksResponse{}
	task := make(map[string]interface{})
PopulateTasks:
	for iter.MapScan(task) {
		t := createTaskInfo(task["transfer"].(map[string]interface{}))
		// Reset task map to get it ready for next scan
		task = make(map[string]interface{})
		// Skip if the task should not be delivered right now
		if t.VisibilityTime.After(currentTimestamp) {
			continue
		}

		newVisibilityTime := currentTimestamp.Add(request.LockTimeout)
		newCQLVisibilityTime := common.UnixNanoToCQLTimestamp(newVisibilityTime.UnixNano())
		newLockToken := uuid.New()
		newDeliveryCount := t.DeliveryCount + 1

		lockQuery := d.session.Query(templateLockTransferTaskQuery,
			t.WorkflowID,
			t.RunID,
			t.TaskID,
			t.TaskList,
			t.TaskType,
			t.ScheduleID,
			newCQLVisibilityTime,
			newLockToken,
			newDeliveryCount,
			newLockToken,
			d.shardID,
			rowTypeTransferTask,
			t.WorkflowID,
			t.RunID,
			t.TaskID,
			t.LockToken)

		previous := make(map[string]interface{})
		applied, err1 := lockQuery.MapScanCAS(previous)
		if err1 != nil || !applied {
			// TODO: log on failure to acquire lock
			continue
		}

		t.VisibilityTime = newVisibilityTime
		t.LockToken = newLockToken
		t.DeliveryCount = newDeliveryCount
		response.Tasks = append(response.Tasks, t)
		if len(response.Tasks) == request.BatchSize {
			break PopulateTasks
		}
	}

	if err := iter.Close(); err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetTransferTasks operation failed. Error: %v", err),
		}
	}

	return response, nil
}

func (d *cassandraPersistence) CompleteTransferTask(request *CompleteTransferTaskRequest) error {
	execution := request.Execution
	query := d.session.Query(templateCompleteTransferTaskQuery,
		d.shardID,
		rowTypeTransferTask,
		execution.GetWorkflowId(),
		execution.GetRunId(),
		request.TaskID,
		request.LockToken)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteTransferTask operation failed. Error: %v", err),
		}
	}

	if !applied {
		if v, ok := previous["lock_token"]; ok {
			actualLockToken := v.(gocql.UUID).String()
			return &ConditionFailedError{
				msg: fmt.Sprintf("Failed to complete task.  Provided lock_token: %v, actual lock_token: %v", request.LockToken,
					actualLockToken),
			}
		}

		return &workflow.EntityNotExistsError{
			Message: fmt.Sprintf("Task not found.  WorkflowId: %v, RunId: %v, TaskId: %v", execution.GetWorkflowId(),
				execution.GetRunId(), request.TaskID),
		}
	}

	return nil
}

func (d *cassandraPersistence) CreateTask(request *CreateTaskRequest) (*CreateTaskResponse, error) {
	cqlNowTimestamp := common.UnixNanoToCQLTimestamp(time.Now().UnixNano())
	taskUUID := uuid.New()
	lockToken := uuid.New()

	var taskID int64
	var taskList string
	var scheduleID int64
	switch request.Data.GetType() {
	case TaskTypeActivity:
		taskID = request.Data.(*ActivityTask).TaskID
		taskList = request.Data.(*ActivityTask).TaskList
		scheduleID = request.Data.(*ActivityTask).ScheduleID
	case TaskTypeDecision:
		taskID = request.Data.(*DecisionTask).TaskID
		taskList = request.Data.(*DecisionTask).TaskList
		scheduleID = request.Data.(*DecisionTask).ScheduleID
	}

	query := d.session.Query(templateCreateTaskQuery,
		request.TaskList,
		request.Data.GetType(),
		request.Execution.GetWorkflowId(),
		request.Execution.GetRunId(),
		taskUUID,
		request.Execution.GetWorkflowId(),
		request.Execution.GetRunId(),
		taskID,
		taskList,
		request.Data.GetType(),
		scheduleID,
		cqlNowTimestamp,
		lockToken,
		0,
		lockToken).Consistency(d.lowConslevel)

	if err := query.Exec(); err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("CreateTask operation failed. Error: %v", err),
		}
	}

	return &CreateTaskResponse{TaskID: taskUUID}, nil
}

func (d *cassandraPersistence) GetTasks(request *GetTasksRequest) (*GetTasksResponse, error) {
	currentTimestamp := time.Now()

	query := d.session.Query(templateGetTasksQuery,
		request.TaskList,
		request.TaskType).Consistency(d.lowConslevel)

	iter := query.Iter()
	if iter == nil {
		return nil, &workflow.InternalServiceError{
			Message: "GetTasks operation failed.  Not able to create query iterator.",
		}
	}

	response := &GetTasksResponse{}
	task := make(map[string]interface{})
PopulateTasks:
	for iter.MapScan(task) {

		taskUUID := task["task_id"].(gocql.UUID).String()
		t := createTaskInfo(task["task"].(map[string]interface{}))
		// Reset task map to get it ready for next scan
		task = make(map[string]interface{})
		// Skip if the task should not be delivered right now
		if t.VisibilityTime.After(currentTimestamp) {
			continue
		}

		newVisibilityTime := currentTimestamp.Add(request.LockTimeout)
		newCQLVisibilityTime := common.UnixNanoToCQLTimestamp(newVisibilityTime.UnixNano())
		newLockToken := uuid.New()
		newDeliveryCount := t.DeliveryCount + 1

		lockQuery := d.session.Query(templateLockTaskQuery,
			t.WorkflowID,
			t.RunID,
			t.TaskID,
			t.TaskList,
			t.TaskType,
			t.ScheduleID,
			newCQLVisibilityTime,
			newLockToken,
			newDeliveryCount,
			newLockToken,
			t.TaskList,
			t.TaskType,
			t.WorkflowID,
			t.RunID,
			taskUUID,
			t.LockToken)

		previous := make(map[string]interface{})
		applied, err1 := lockQuery.MapScanCAS(previous)
		if err1 != nil || !applied {
			// TODO: log on failure to acquire lock
			continue
		}

		t.VisibilityTime = newVisibilityTime
		t.LockToken = newLockToken
		t.DeliveryCount = newDeliveryCount
		response.Tasks = append(response.Tasks, &TaskInfoWithID{TaskUUID: taskUUID, Info: t})
		if len(response.Tasks) == request.BatchSize {
			break PopulateTasks
		}
	}

	if err := iter.Close(); err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetTasks operation failed. Error: %v", err),
		}
	}

	return response, nil
}

func (d *cassandraPersistence) CompleteTask(request *CompleteTaskRequest) error {
	execution := request.Execution
	query := d.session.Query(templateCompleteTaskQuery,
		request.TaskList,
		request.TaskType,
		execution.GetWorkflowId(),
		execution.GetRunId(),
		request.TaskID,
		request.LockToken)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteTask operation failed. Error: %v", err),
		}
	}

	if !applied {
		if v, ok := previous["lock_token"]; ok {
			actualLockToken := v.(gocql.UUID).String()
			return &ConditionFailedError{
				msg: fmt.Sprintf("Failed to complete task.  Provided lock_token: %v, actual lock_token: %v", request.LockToken,
					actualLockToken),
			}
		}

		return &workflow.EntityNotExistsError{
			Message: fmt.Sprintf("Task not found.  WorkflowId: %v, RunId: %v, TaskId: %v", execution.GetWorkflowId(),
				execution.GetRunId(), request.TaskID),
		}
	}

	return nil
}

func (d *cassandraPersistence) createTransferTasks(batch *gocql.Batch, transferTasks []Task, workflowID string,
	runID string, cqlNowTimestamp int64) {
	for _, task := range transferTasks {
		var transferTaskID int64
		var taskList string
		var scheduleID int64
		switch task.GetType() {
		case TaskTypeActivity:
			transferTaskID = task.(*ActivityTask).TaskID
			taskList = task.(*ActivityTask).TaskList
			scheduleID = task.(*ActivityTask).ScheduleID
		case TaskTypeDecision:
			transferTaskID = task.(*DecisionTask).TaskID
			taskList = task.(*DecisionTask).TaskList
			scheduleID = task.(*DecisionTask).ScheduleID
		}

		lockToken := uuid.New()
		batch.Query(templateCreateTransferTaskQuery,
			d.shardID,
			rowTypeTransferTask,
			workflowID,
			runID,
			workflowID,
			runID,
			transferTaskID,
			taskList,
			task.GetType(),
			scheduleID,
			cqlNowTimestamp,
			lockToken,
			0,
			transferTaskID,
			lockToken)
	}
}

func createShardInfo(result map[string]interface{}) *ShardInfo {
	info := &ShardInfo{}
	for k, v := range result {
		switch k {
		case "shard_id":
			info.ShardID = v.(int)
		case "range_id":
			info.RangeID = v.(int64)
		case "transfer_ack_level":
			info.TransferAckLevel = v.(int64)
		}
	}

	return info
}

func createWorkflowExecutionInfo(result map[string]interface{}) *WorkflowExecutionInfo {
	info := &WorkflowExecutionInfo{}
	for k, v := range result {
		switch k {
		case "workflow_id":
			info.WorkflowID = v.(string)
		case "run_id":
			info.RunID = v.(gocql.UUID).String()
		case "task_list":
			info.TaskList = v.(string)
		case "history":
			info.History = v.([]byte)
		case "execution_context":
			info.ExecutionContext = v.([]byte)
		case "state":
			info.State = v.(int)
		case "next_event_id":
			info.NextEventID = v.(int64)
		case "last_processed_event":
			info.LastProcessedEvent = v.(int64)
		case "last_updated_time":
			info.LastUpdatedTimestamp = v.(time.Time)
		case "decision_pending":
			info.DecisionPending = v.(bool)
		}
	}

	return info
}

func createTaskInfo(result map[string]interface{}) *TaskInfo {
	info := &TaskInfo{}
	for k, v := range result {
		switch k {
		case "workflow_id":
			info.WorkflowID = v.(string)
		case "run_id":
			info.RunID = v.(gocql.UUID).String()
		case "task_id":
			info.TaskID = v.(int64)
		case "task_list":
			info.TaskList = v.(string)
		case "type":
			info.TaskType = v.(int)
		case "schedule_id":
			info.ScheduleID = v.(int64)
		case "visibility_time":
			info.VisibilityTime = v.(time.Time)
		case "lock_token":
			info.LockToken = v.(gocql.UUID).String()
		case "delivery_count":
			info.DeliveryCount = v.(int)
		}
	}

	return info
}