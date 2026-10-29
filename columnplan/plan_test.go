package columnplan_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/columnplan"
)

type EmployeeRow struct {
	ID        string    `stencil:"col:0,key:emp_id,required"`
	Salary    int64     `stencil:"col:1,key:salary,currency"`
	HiredAt   time.Time `stencil:"col:2,key:hired_at,iso8601"`
	Department string   `stencil:"col:3,key:dept"`
}

func TestColumnPlan_PermutedAndUnmapped(t *testing.T) {
	s, err := stencil.Compile[EmployeeRow]()
	if err != nil {
		t.Fatal(err)
	}

	// Permuted headers with extra unmapped column "notes" and missing optional "dept"
	headers := []string{"salary", "notes", "emp_id", "hired_at"}
	plan, err := columnplan.CompileHeaderPlanStrings(headers, s)
	if err != nil {
		t.Fatalf("failed to compile column plan: %v", err)
	}

	if plan.TotalColumns() != 4 {
		t.Errorf("expected 4 total columns, got %d", plan.TotalColumns())
	}
	if plan.MappedCount() != 3 {
		t.Errorf("expected 3 mapped columns, got %d", plan.MappedCount())
	}
	if len(plan.UnmappedColumns()) != 1 || plan.UnmappedColumns()[0] != "notes" {
		t.Errorf("expected unmapped [notes], got %v", plan.UnmappedColumns())
	}

	// Apply row cells according to physical column positions
	var emp EmployeeRow
	var rowErr stencil.RowError

	// col 0: salary = $85,000.00
	if !plan.ApplyCell(&emp, 1, 0, []byte("$85,000.00"), &rowErr) {
		t.Fatalf("col 0 failed: %+v", rowErr)
	}
	// col 1: notes = "ignored text"
	if !plan.ApplyCell(&emp, 1, 1, []byte("ignored text"), &rowErr) {
		t.Fatalf("col 1 failed: %+v", rowErr)
	}
	// col 2: emp_id = "E-999"
	if !plan.ApplyCell(&emp, 1, 2, []byte("E-999"), &rowErr) {
		t.Fatalf("col 2 failed: %+v", rowErr)
	}
	// col 3: hired_at = "2026-05-01T08:00:00Z"
	if !plan.ApplyCell(&emp, 1, 3, []byte("2026-05-01T08:00:00Z"), &rowErr) {
		t.Fatalf("col 3 failed: %+v", rowErr)
	}

	if emp.ID != "E-999" {
		t.Errorf("expected emp_id E-999, got %q", emp.ID)
	}
	if emp.Salary != 8500000 {
		t.Errorf("expected salary 8500000, got %d", emp.Salary)
	}
	if emp.HiredAt.Year() != 2026 || emp.HiredAt.Month() != 5 {
		t.Errorf("expected hired_at 2026-05, got %v", emp.HiredAt)
	}
}

func TestColumnPlan_MissingRequiredFailsPhase1(t *testing.T) {
	s, err := stencil.Compile[EmployeeRow]()
	if err != nil {
		t.Fatal(err)
	}

	// Missing required "emp_id"
	headers := []string{"salary", "dept", "hired_at"}
	_, err = columnplan.CompileHeaderPlanStrings(headers, s)
	if !errors.Is(err, columnplan.ErrMissingRequiredColumn) {
		t.Fatalf("expected ErrMissingRequiredColumn, got %v", err)
	}
}

func TestColumnPlan_DuplicateHeaderFailsPhase1(t *testing.T) {
	s, err := stencil.Compile[EmployeeRow]()
	if err != nil {
		t.Fatal(err)
	}

	headers := []string{"emp_id", "salary", "salary"}
	_, err = columnplan.CompileHeaderPlanStrings(headers, s)
	if !errors.Is(err, columnplan.ErrDuplicateHeaderColumn) {
		t.Fatalf("expected ErrDuplicateHeaderColumn, got %v", err)
	}
}

func TestColumnPlan_ZeroAllocApplyCell(t *testing.T) {
	s, err := stencil.Compile[EmployeeRow]()
	if err != nil {
		t.Fatal(err)
	}

	headers := []string{"emp_id", "salary", "hired_at"}
	plan, err := columnplan.CompileHeaderPlanStrings(headers, s)
	if err != nil {
		t.Fatal(err)
	}

	var emp EmployeeRow
	var rowErr stencil.RowError
	val := []byte("E-123")

	allocs := testing.AllocsPerRun(100, func() {
		_ = plan.ApplyCell(&emp, 1, 0, val, &rowErr)
	})

	if allocs > 0 {
		t.Fatalf("expected 0 allocs per ApplyCell, got %f", allocs)
	}
}

func BenchmarkColumnPlan_ApplyCell(b *testing.B) {
	s, err := stencil.Compile[EmployeeRow]()
	if err != nil {
		b.Fatal(err)
	}

	headers := []string{"emp_id", "salary", "hired_at"}
	plan, err := columnplan.CompileHeaderPlanStrings(headers, s)
	if err != nil {
		b.Fatal(err)
	}

	var emp EmployeeRow
	var rowErr stencil.RowError
	val := []byte("E-123")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = plan.ApplyCell(&emp, 1, 0, val, &rowErr)
	}
}
