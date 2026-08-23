package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// Task column list for SELECT queries.
const taskColumns = `id, session_id, prompt, status, max_retries, retry_count,
	timeout_seconds, tenant_id, labels, annotations, created_at, updated_at`

// TaskRun column list for SELECT queries.
const taskRunColumns = `id, task_id, attempt, runner_id, status, error, exit_code,
	tokens_input, tokens_output, tenant_id, queued_at, assigned_at, started_at, ended_at, updated_at`

// ScheduledTask column list for SELECT queries.
const scheduledTaskColumns = `id, session_id, name, description, cron_expression, timezone,
	prompt_template, timeout_seconds, max_retries, status, next_run_at, last_run_at,
	last_task_id, run_count, failure_count, on_failure, max_consecutive_failures,
	consecutive_failures, tenant_id, labels, annotations, created_at, updated_at`

// =============================================================================
// Task CRUD
// =============================================================================

// CreateTask creates a new task.
func (s *Store) CreateTask(ctx context.Context, task *store.Task) error {
	return createTask(ctx, s.pool, task)
}

// CreateTask creates a new task within a transaction.
func (t *Tx) CreateTask(ctx context.Context, task *store.Task) error {
	return createTask(ctx, t.tx, task)
}

func createTask(ctx context.Context, q querier, task *store.Task) error {
	if task.ID == "" {
		task.ID = id.Task()
	}

	query := `
		INSERT INTO tasks (
			id, session_id, prompt, status, max_retries, retry_count,
			timeout_seconds, tenant_id, labels, annotations, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		task.ID, task.SessionID, task.Prompt, task.Status, task.MaxRetries, task.RetryCount,
		task.TimeoutSeconds, task.TenantID, emptyJSONObject(task.Labels), emptyJSONObject(task.Annotations),
	).Scan(&task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return handlePgError(err, "task", task.ID)
	}
	return nil
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(ctx context.Context, taskID string) (*store.Task, error) {
	return getTask(ctx, s.pool, taskID)
}

// GetTask retrieves a task by ID within a transaction.
func (t *Tx) GetTask(ctx context.Context, taskID string) (*store.Task, error) {
	return getTask(ctx, t.tx, taskID)
}

func getTask(ctx context.Context, q querier, taskID string) (*store.Task, error) {
	query := fmt.Sprintf(`SELECT %s FROM tasks WHERE id = $1`, taskColumns)
	row := q.QueryRow(ctx, query, taskID)
	return scanTask(row, taskID)
}

// ListTasks retrieves tasks with optional filtering.
func (s *Store) ListTasks(ctx context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return listTasks(ctx, s.pool, opts)
}

// ListTasks retrieves tasks within a transaction.
func (t *Tx) ListTasks(ctx context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return listTasks(ctx, t.tx, opts)
}

func listTasks(ctx context.Context, q querier, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.SessionID != nil {
		conditions = append(conditions, fmt.Sprintf("session_id = $%d", argNum))
		args = append(args, *opts.SessionID)
		argNum++
	}
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := taskSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tasks %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting tasks: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM tasks %s
		ORDER BY %s
		LIMIT $%d`,
		taskColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*store.Task
	for rows.Next() {
		task, err := scanTaskFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tasks: %w", err)
	}

	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}

	return &store.ListResult[store.Task]{
		Items:      tasks,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateTask updates task fields.
func (s *Store) UpdateTask(ctx context.Context, taskID string, updates store.TaskUpdates) error {
	return updateTask(ctx, s.pool, taskID, updates)
}

// UpdateTask updates task fields within a transaction.
func (t *Tx) UpdateTask(ctx context.Context, taskID string, updates store.TaskUpdates) error {
	return updateTask(ctx, t.tx, taskID, updates)
}

func updateTask(ctx context.Context, q querier, taskID string, updates store.TaskUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.RetryCount != nil {
		setClauses = append(setClauses, fmt.Sprintf("retry_count = $%d", argNum))
		args = append(args, *updates.RetryCount)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE tasks SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, taskID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "task", taskID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "task", ID: taskID}
	}

	return nil
}

// DeleteTask deletes a task.
func (s *Store) DeleteTask(ctx context.Context, taskID string) error {
	return deleteTask(ctx, s.pool, taskID)
}

// DeleteTask deletes a task within a transaction.
func (t *Tx) DeleteTask(ctx context.Context, taskID string) error {
	return deleteTask(ctx, t.tx, taskID)
}

func deleteTask(ctx context.Context, q querier, taskID string) error {
	query := `DELETE FROM tasks WHERE id = $1`
	result, err := q.Exec(ctx, query, taskID)
	if err != nil {
		return handlePgError(err, "task", taskID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "task", ID: taskID}
	}

	return nil
}

func scanTask(row pgx.Row, identifier string) (*store.Task, error) {
	var t store.Task
	err := row.Scan(
		&t.ID, &t.SessionID, &t.Prompt, &t.Status, &t.MaxRetries, &t.RetryCount,
		&t.TimeoutSeconds, &t.TenantID, &t.Labels, &t.Annotations, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "task", ID: identifier}
		}
		return nil, fmt.Errorf("scanning task: %w", err)
	}
	return &t, nil
}

func scanTaskFromRows(rows pgx.Rows) (*store.Task, error) {
	var t store.Task
	err := rows.Scan(
		&t.ID, &t.SessionID, &t.Prompt, &t.Status, &t.MaxRetries, &t.RetryCount,
		&t.TimeoutSeconds, &t.TenantID, &t.Labels, &t.Annotations, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// =============================================================================
// TaskRun CRUD
// =============================================================================

// CreateTaskRun creates a new task run.
func (s *Store) CreateTaskRun(ctx context.Context, run *store.TaskRun) error {
	return createTaskRun(ctx, s.pool, run)
}

// CreateTaskRun creates a new task run within a transaction.
func (t *Tx) CreateTaskRun(ctx context.Context, run *store.TaskRun) error {
	return createTaskRun(ctx, t.tx, run)
}

func createTaskRun(ctx context.Context, q querier, run *store.TaskRun) error {
	if run.ID == "" {
		run.ID = id.TaskRun()
	}

	query := `
		INSERT INTO task_runs (
			id, task_id, attempt, runner_id, status, error, exit_code,
			tokens_input, tokens_output, tenant_id, queued_at, assigned_at,
			started_at, ended_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), $11, $12, $13, NOW()
		)
		RETURNING queued_at, updated_at`

	err := q.QueryRow(ctx, query,
		run.ID, run.TaskID, run.Attempt, run.RunnerID, run.Status, run.Error, run.ExitCode,
		run.TokensInput, run.TokensOutput, run.TenantID, run.AssignedAt, run.StartedAt, run.EndedAt,
	).Scan(&run.QueuedAt, &run.UpdatedAt)

	if err != nil {
		return handlePgError(err, "task_run", run.ID)
	}
	return nil
}

// GetTaskRun retrieves a task run by ID.
func (s *Store) GetTaskRun(ctx context.Context, runID string) (*store.TaskRun, error) {
	return getTaskRun(ctx, s.pool, runID)
}

// GetTaskRun retrieves a task run by ID within a transaction.
func (t *Tx) GetTaskRun(ctx context.Context, runID string) (*store.TaskRun, error) {
	return getTaskRun(ctx, t.tx, runID)
}

func getTaskRun(ctx context.Context, q querier, runID string) (*store.TaskRun, error) {
	query := fmt.Sprintf(`SELECT %s FROM task_runs WHERE id = $1`, taskRunColumns)
	row := q.QueryRow(ctx, query, runID)
	return scanTaskRun(row, runID)
}

// GetTaskRunByTaskAndAttempt retrieves a task run by task ID and attempt number.
func (s *Store) GetTaskRunByTaskAndAttempt(ctx context.Context, taskID string, attempt int) (*store.TaskRun, error) {
	return getTaskRunByTaskAndAttempt(ctx, s.pool, taskID, attempt)
}

// GetTaskRunByTaskAndAttempt retrieves a task run within a transaction.
func (t *Tx) GetTaskRunByTaskAndAttempt(ctx context.Context, taskID string, attempt int) (*store.TaskRun, error) {
	return getTaskRunByTaskAndAttempt(ctx, t.tx, taskID, attempt)
}

func getTaskRunByTaskAndAttempt(ctx context.Context, q querier, taskID string, attempt int) (*store.TaskRun, error) {
	query := fmt.Sprintf(`SELECT %s FROM task_runs WHERE task_id = $1 AND attempt = $2`, taskRunColumns)
	row := q.QueryRow(ctx, query, taskID, attempt)
	return scanTaskRun(row, fmt.Sprintf("%s/attempt/%d", taskID, attempt))
}

// ListTaskRuns retrieves task runs with optional filtering.
func (s *Store) ListTaskRuns(ctx context.Context, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return listTaskRuns(ctx, s.pool, opts)
}

// ListTaskRuns retrieves task runs within a transaction.
func (t *Tx) ListTaskRuns(ctx context.Context, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return listTaskRuns(ctx, t.tx, opts)
}

func listTaskRuns(ctx context.Context, q querier, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argNum))
		args = append(args, *opts.TaskID)
		argNum++
	}
	if opts.RunnerID != nil {
		conditions = append(conditions, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *opts.RunnerID)
		argNum++
	}
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := taskRunSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM task_runs %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting task_runs: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM task_runs %s
		ORDER BY %s
		LIMIT $%d`,
		taskRunColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying task_runs: %w", err)
	}
	defer rows.Close()

	var runs []*store.TaskRun
	for rows.Next() {
		run, err := scanTaskRunFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task_run: %w", err)
		}
		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task_runs: %w", err)
	}

	hasMore := len(runs) > limit
	if hasMore {
		runs = runs[:limit]
	}

	return &store.ListResult[store.TaskRun]{
		Items:      runs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateTaskRun updates task run fields.
func (s *Store) UpdateTaskRun(ctx context.Context, runID string, updates store.TaskRunUpdates) error {
	return updateTaskRun(ctx, s.pool, runID, updates)
}

// UpdateTaskRun updates task run fields within a transaction.
func (t *Tx) UpdateTaskRun(ctx context.Context, runID string, updates store.TaskRunUpdates) error {
	return updateTaskRun(ctx, t.tx, runID, updates)
}

func updateTaskRun(ctx context.Context, q querier, runID string, updates store.TaskRunUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.RunnerID != nil {
		setClauses = append(setClauses, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *updates.RunnerID)
		argNum++
	}
	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.Error != nil {
		setClauses = append(setClauses, fmt.Sprintf("error = $%d", argNum))
		args = append(args, *updates.Error)
		argNum++
	}
	if updates.ExitCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("exit_code = $%d", argNum))
		args = append(args, *updates.ExitCode)
		argNum++
	}
	if updates.TokensInput != nil {
		setClauses = append(setClauses, fmt.Sprintf("tokens_input = $%d", argNum))
		args = append(args, *updates.TokensInput)
		argNum++
	}
	if updates.TokensOutput != nil {
		setClauses = append(setClauses, fmt.Sprintf("tokens_output = $%d", argNum))
		args = append(args, *updates.TokensOutput)
		argNum++
	}
	if updates.AssignedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_at = $%d", argNum))
		args = append(args, *updates.AssignedAt)
		argNum++
	}
	if updates.StartedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("started_at = $%d", argNum))
		args = append(args, *updates.StartedAt)
		argNum++
	}
	if updates.EndedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("ended_at = $%d", argNum))
		args = append(args, *updates.EndedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE task_runs SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, runID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "task_run", runID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "task_run", ID: runID}
	}

	return nil
}

func scanTaskRun(row pgx.Row, identifier string) (*store.TaskRun, error) {
	var r store.TaskRun
	err := row.Scan(
		&r.ID, &r.TaskID, &r.Attempt, &r.RunnerID, &r.Status, &r.Error, &r.ExitCode,
		&r.TokensInput, &r.TokensOutput, &r.TenantID, &r.QueuedAt, &r.AssignedAt,
		&r.StartedAt, &r.EndedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "task_run", ID: identifier}
		}
		return nil, fmt.Errorf("scanning task_run: %w", err)
	}
	return &r, nil
}

func scanTaskRunFromRows(rows pgx.Rows) (*store.TaskRun, error) {
	var r store.TaskRun
	err := rows.Scan(
		&r.ID, &r.TaskID, &r.Attempt, &r.RunnerID, &r.Status, &r.Error, &r.ExitCode,
		&r.TokensInput, &r.TokensOutput, &r.TenantID, &r.QueuedAt, &r.AssignedAt,
		&r.StartedAt, &r.EndedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// =============================================================================
// ScheduledTask CRUD
// =============================================================================

// CreateScheduledTask creates a new scheduled task.
func (s *Store) CreateScheduledTask(ctx context.Context, task *store.ScheduledTask) error {
	return createScheduledTask(ctx, s.pool, task)
}

// CreateScheduledTask creates a new scheduled task within a transaction.
func (t *Tx) CreateScheduledTask(ctx context.Context, task *store.ScheduledTask) error {
	return createScheduledTask(ctx, t.tx, task)
}

func createScheduledTask(ctx context.Context, q querier, task *store.ScheduledTask) error {
	if task.ID == "" {
		task.ID = id.ScheduledTask()
	}

	query := `
		INSERT INTO scheduled_tasks (
			id, session_id, name, description, cron_expression, timezone,
			prompt_template, timeout_seconds, max_retries, status, next_run_at, last_run_at,
			last_task_id, run_count, failure_count, on_failure, max_consecutive_failures,
			consecutive_failures, tenant_id, labels, annotations, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		task.ID, task.SessionID, task.Name, task.Description, task.CronExpression, task.Timezone,
		task.PromptTemplate, task.TimeoutSeconds, task.MaxRetries, task.Status, task.NextRunAt, task.LastRunAt,
		task.LastTaskID, task.RunCount, task.FailureCount, task.OnFailure, task.MaxConsecutiveFailures,
		task.ConsecutiveFailures, task.TenantID, emptyJSONObject(task.Labels), emptyJSONObject(task.Annotations),
	).Scan(&task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return handlePgError(err, "scheduled_task", task.ID)
	}
	return nil
}

// GetScheduledTask retrieves a scheduled task by ID.
func (s *Store) GetScheduledTask(ctx context.Context, taskID string) (*store.ScheduledTask, error) {
	return getScheduledTask(ctx, s.pool, taskID)
}

// GetScheduledTask retrieves a scheduled task by ID within a transaction.
func (t *Tx) GetScheduledTask(ctx context.Context, taskID string) (*store.ScheduledTask, error) {
	return getScheduledTask(ctx, t.tx, taskID)
}

func getScheduledTask(ctx context.Context, q querier, taskID string) (*store.ScheduledTask, error) {
	query := fmt.Sprintf(`SELECT %s FROM scheduled_tasks WHERE id = $1`, scheduledTaskColumns)
	row := q.QueryRow(ctx, query, taskID)
	return scanScheduledTask(row, taskID)
}

// ListScheduledTasks retrieves scheduled tasks with optional filtering.
func (s *Store) ListScheduledTasks(ctx context.Context, opts store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return listScheduledTasks(ctx, s.pool, opts)
}

// ListScheduledTasks retrieves scheduled tasks within a transaction.
func (t *Tx) ListScheduledTasks(ctx context.Context, opts store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return listScheduledTasks(ctx, t.tx, opts)
}

func listScheduledTasks(ctx context.Context, q querier, opts store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.SessionID != nil {
		conditions = append(conditions, fmt.Sprintf("session_id = $%d", argNum))
		args = append(args, *opts.SessionID)
		argNum++
	}
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := scheduledTaskSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM scheduled_tasks %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting scheduled_tasks: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM scheduled_tasks %s
		ORDER BY %s
		LIMIT $%d`,
		scheduledTaskColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying scheduled_tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*store.ScheduledTask
	for rows.Next() {
		task, err := scanScheduledTaskFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning scheduled_task: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating scheduled_tasks: %w", err)
	}

	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}

	return &store.ListResult[store.ScheduledTask]{
		Items:      tasks,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateScheduledTask updates scheduled task fields.
func (s *Store) UpdateScheduledTask(ctx context.Context, taskID string, updates store.ScheduledTaskUpdates) error {
	return updateScheduledTask(ctx, s.pool, taskID, updates)
}

// UpdateScheduledTask updates scheduled task fields within a transaction.
func (t *Tx) UpdateScheduledTask(ctx context.Context, taskID string, updates store.ScheduledTaskUpdates) error {
	return updateScheduledTask(ctx, t.tx, taskID, updates)
}

func updateScheduledTask(ctx context.Context, q querier, taskID string, updates store.ScheduledTaskUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argNum))
		args = append(args, *updates.Description)
		argNum++
	}
	if updates.CronExpression != nil {
		setClauses = append(setClauses, fmt.Sprintf("cron_expression = $%d", argNum))
		args = append(args, *updates.CronExpression)
		argNum++
	}
	if updates.Timezone != nil {
		setClauses = append(setClauses, fmt.Sprintf("timezone = $%d", argNum))
		args = append(args, *updates.Timezone)
		argNum++
	}
	if updates.PromptTemplate != nil {
		setClauses = append(setClauses, fmt.Sprintf("prompt_template = $%d", argNum))
		args = append(args, *updates.PromptTemplate)
		argNum++
	}
	if updates.TimeoutSeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("timeout_seconds = $%d", argNum))
		args = append(args, *updates.TimeoutSeconds)
		argNum++
	}
	if updates.MaxRetries != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_retries = $%d", argNum))
		args = append(args, *updates.MaxRetries)
		argNum++
	}
	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.NextRunAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("next_run_at = $%d", argNum))
		args = append(args, *updates.NextRunAt)
		argNum++
	}
	if updates.LastRunAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_run_at = $%d", argNum))
		args = append(args, *updates.LastRunAt)
		argNum++
	}
	if updates.LastTaskID != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_task_id = $%d", argNum))
		args = append(args, *updates.LastTaskID)
		argNum++
	}
	if updates.RunCount != nil {
		setClauses = append(setClauses, fmt.Sprintf("run_count = $%d", argNum))
		args = append(args, *updates.RunCount)
		argNum++
	}
	if updates.FailureCount != nil {
		setClauses = append(setClauses, fmt.Sprintf("failure_count = $%d", argNum))
		args = append(args, *updates.FailureCount)
		argNum++
	}
	if updates.OnFailure != nil {
		setClauses = append(setClauses, fmt.Sprintf("on_failure = $%d", argNum))
		args = append(args, *updates.OnFailure)
		argNum++
	}
	if updates.MaxConsecutiveFailures != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_consecutive_failures = $%d", argNum))
		args = append(args, *updates.MaxConsecutiveFailures)
		argNum++
	}
	if updates.ConsecutiveFailures != nil {
		setClauses = append(setClauses, fmt.Sprintf("consecutive_failures = $%d", argNum))
		args = append(args, *updates.ConsecutiveFailures)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE scheduled_tasks SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, taskID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "scheduled_task", taskID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "scheduled_task", ID: taskID}
	}

	return nil
}

// DeleteScheduledTask deletes a scheduled task.
func (s *Store) DeleteScheduledTask(ctx context.Context, taskID string) error {
	return deleteScheduledTask(ctx, s.pool, taskID)
}

// DeleteScheduledTask deletes a scheduled task within a transaction.
func (t *Tx) DeleteScheduledTask(ctx context.Context, taskID string) error {
	return deleteScheduledTask(ctx, t.tx, taskID)
}

func deleteScheduledTask(ctx context.Context, q querier, taskID string) error {
	query := `DELETE FROM scheduled_tasks WHERE id = $1`
	result, err := q.Exec(ctx, query, taskID)
	if err != nil {
		return handlePgError(err, "scheduled_task", taskID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "scheduled_task", ID: taskID}
	}

	return nil
}

// GetDueScheduledTasks retrieves scheduled tasks that are due to run.
// It selects tasks where status='active' and next_run_at <= now.
func (s *Store) GetDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]*store.ScheduledTask, error) {
	return getDueScheduledTasks(ctx, s.pool, now, limit)
}

// GetDueScheduledTasks retrieves due scheduled tasks within a transaction.
func (t *Tx) GetDueScheduledTasks(ctx context.Context, now time.Time, limit int) ([]*store.ScheduledTask, error) {
	return getDueScheduledTasks(ctx, t.tx, now, limit)
}

func getDueScheduledTasks(ctx context.Context, q querier, now time.Time, limit int) ([]*store.ScheduledTask, error) {
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
		SELECT %s FROM scheduled_tasks
		WHERE status = 'active' AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at ASC
		LIMIT $2
	`, scheduledTaskColumns)

	rows, err := q.Query(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("querying due scheduled_tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*store.ScheduledTask
	for rows.Next() {
		task, err := scanScheduledTaskFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning scheduled_task: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating scheduled_tasks: %w", err)
	}

	return tasks, nil
}

func scanScheduledTask(row pgx.Row, identifier string) (*store.ScheduledTask, error) {
	var t store.ScheduledTask
	err := row.Scan(
		&t.ID, &t.SessionID, &t.Name, &t.Description, &t.CronExpression, &t.Timezone,
		&t.PromptTemplate, &t.TimeoutSeconds, &t.MaxRetries, &t.Status, &t.NextRunAt, &t.LastRunAt,
		&t.LastTaskID, &t.RunCount, &t.FailureCount, &t.OnFailure, &t.MaxConsecutiveFailures,
		&t.ConsecutiveFailures, &t.TenantID, &t.Labels, &t.Annotations, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "scheduled_task", ID: identifier}
		}
		return nil, fmt.Errorf("scanning scheduled_task: %w", err)
	}
	return &t, nil
}

func scanScheduledTaskFromRows(rows pgx.Rows) (*store.ScheduledTask, error) {
	var t store.ScheduledTask
	err := rows.Scan(
		&t.ID, &t.SessionID, &t.Name, &t.Description, &t.CronExpression, &t.Timezone,
		&t.PromptTemplate, &t.TimeoutSeconds, &t.MaxRetries, &t.Status, &t.NextRunAt, &t.LastRunAt,
		&t.LastTaskID, &t.RunCount, &t.FailureCount, &t.OnFailure, &t.MaxConsecutiveFailures,
		&t.ConsecutiveFailures, &t.TenantID, &t.Labels, &t.Annotations, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
