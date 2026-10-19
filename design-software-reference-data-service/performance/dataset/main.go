package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	allowedMutation  = "true"
	expectedHost     = "localhost"
	expectedPort     = "15436"
	expectedDatabase = "design-software-develop"
)

type volumes struct {
	macroregions, microregions, departments, municipalities int
	trainingCenters, institutionalUnits                     int
	catalogs, catalogDetails, parameters                    int
}

var profiles = map[string]volumes{
	"smoke": {
		macroregions: 2, microregions: 4, departments: 10, municipalities: 100,
		trainingCenters: 10, institutionalUnits: 20, catalogs: 5, catalogDetails: 50, parameters: 20,
	},
	"baseline": {
		macroregions: 10, microregions: 50, departments: 500, municipalities: 100000,
		trainingCenters: 10000, institutionalUnits: 20000, catalogs: 1000, catalogDetails: 100000, parameters: 10000,
	},
}

func main() {
	action := flag.String("action", "load", "load or cleanup")
	profile := flag.String("profile", "smoke", "smoke or baseline")
	flag.Parse()

	if err := run(*action, *profile); err != nil {
		fmt.Fprintln(os.Stderr, "R2 dataset aborted:", err)
		os.Exit(1)
	}
}

func run(action, profile string) error {
	if !strings.EqualFold(os.Getenv("R2_ALLOW_DATASET_MUTATION"), allowedMutation) {
		return errors.New("set R2_ALLOW_DATASET_MUTATION=true to allow dataset mutation")
	}

	vol, ok := profiles[strings.ToLower(profile)]
	if !ok {
		return fmt.Errorf("unknown profile %q; use smoke or baseline", profile)
	}
	if action != "load" && action != "cleanup" {
		return fmt.Errorf("unknown action %q; use load or cleanup", action)
	}

	dsn, err := guardedDSN()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(context.Background())

	var currentDatabase string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&currentDatabase); err != nil {
		return fmt.Errorf("verify database: %w", err)
	}
	if currentDatabase != expectedDatabase {
		return fmt.Errorf("unexpected database %q", currentDatabase)
	}

	if action == "cleanup" {
		return cleanup(ctx, conn)
	}
	return load(ctx, conn, vol, profile)
}

func guardedDSN() (string, error) {
	dsn := os.Getenv("R2_DATABASE_URL")
	if dsn == "" {
		return "", errors.New("R2_DATABASE_URL is required and must point to the isolated database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("invalid R2_DATABASE_URL: %w", err)
	}
	if parsed.Hostname() != expectedHost && parsed.Hostname() != "127.0.0.1" {
		return "", fmt.Errorf("refusing database host %q; expected localhost", parsed.Hostname())
	}
	if parsed.Port() != expectedPort {
		return "", fmt.Errorf("refusing database port %q; expected %s", parsed.Port(), expectedPort)
	}
	if strings.TrimPrefix(parsed.Path, "/") != expectedDatabase {
		return "", fmt.Errorf("refusing database %q; expected %s", strings.TrimPrefix(parsed.Path, "/"), expectedDatabase)
	}
	if strings.Contains(strings.ToLower(dsn), "ds-develop") || strings.Contains(strings.ToLower(dsn), ":15432") {
		return "", errors.New("refusing a known ds-develop target")
	}
	return dsn, nil
}

func load(ctx context.Context, conn *pgx.Conn, v volumes, profile string) error {
	if existing, err := r2Rows(ctx, conn); err != nil {
		return err
	} else if existing > 0 {
		return fmt.Errorf("R2 dataset already exists (%d rows); run explicit cleanup before loading again", existing)
	}

	macroIDs := make([]string, v.macroregions)
	microIDs := make([]string, v.microregions)
	departmentIDs := make([]string, v.departments)
	municipalityIDs := make([]string, v.municipalities)
	centerIDs := make([]string, v.trainingCenters)
	catalogIDs := make([]string, v.catalogs)

	for i := range macroIDs {
		macroIDs[i] = datasetUUID(0x22000000, i)
	}
	for i := range microIDs {
		microIDs[i] = datasetUUID(0x23000000, i)
	}
	for i := range departmentIDs {
		departmentIDs[i] = datasetUUID(0x24000000, i)
	}
	for i := range municipalityIDs {
		municipalityIDs[i] = datasetUUID(0x25000000, i)
	}
	for i := range centerIDs {
		centerIDs[i] = datasetUUID(0x26000000, i)
	}
	for i := range catalogIDs {
		catalogIDs[i] = datasetUUID(0x28000000, i)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := copyRows(ctx, tx, "institutional_structure.macroregion", []string{"id", "name", "code", "is_active"}, macroRows(macroIDs)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "institutional_structure.microregion", []string{"id", "macroregion_id", "name", "code", "is_active"}, microRows(microIDs, macroIDs)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "institutional_structure.department", []string{"id", "microregion_id", "name", "dane_code", "is_active"}, departmentRows(departmentIDs, microIDs)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "institutional_structure.municipality", []string{"id", "department_id", "name", "dane_code", "is_active"}, municipalityRows(municipalityIDs, departmentIDs)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "institutional_structure.training_center", []string{"id", "municipality_id", "center_code", "name", "is_active"}, centerRows(centerIDs, municipalityIDs)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "institutional_structure.institutional_unit", []string{"id", "training_center_id", "name", "unit_type", "is_active"}, unitRows(centerIDs, v.institutionalUnits)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "parameterization.catalog", []string{"id", "code", "name", "description", "is_active"}, catalogRows(catalogIDs)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "parameterization.catalog_detail", []string{"id", "catalog_id", "code", "label", "display_order", "is_active"}, detailRows(catalogIDs, v.catalogDetails)); err != nil {
		return err
	}
	if err := copyRows(ctx, tx, "parameterization.parameter", []string{"id", "key", "value", "value_type", "description"}, parameterRows(v.parameters)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Printf("Loaded R2 %s dataset: municipalities=%d catalogs=%d catalog_details=%d parameters=%d\n", profile, v.municipalities, v.catalogs, v.catalogDetails, v.parameters)
	fmt.Printf("R2_DEPARTMENT_ID=%s\nR2_CATALOG_ID=%s\n", departmentIDs[0], catalogIDs[0])
	return nil
}

func copyRows(ctx context.Context, tx pgx.Tx, table string, columns []string, rows [][]any) error {
	if _, err := tx.CopyFrom(ctx, pgx.Identifier(strings.Split(table, ".")), columns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("load %s: %w", table, err)
	}
	return nil
}

func cleanup(ctx context.Context, conn *pgx.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cleanup: %w", err)
	}
	defer tx.Rollback(ctx)
	statements := []string{
		"DELETE FROM institutional_structure.institutional_unit WHERE name LIKE 'R2 Performance Unit %'",
		"DELETE FROM institutional_structure.training_center WHERE name LIKE 'R2 Performance Training Center %'",
		"DELETE FROM institutional_structure.municipality WHERE name LIKE 'R2 Performance Municipality %'",
		"DELETE FROM institutional_structure.department WHERE name LIKE 'R2 Performance Department %'",
		"DELETE FROM institutional_structure.microregion WHERE name LIKE 'R2 Performance Microregion %'",
		"DELETE FROM institutional_structure.macroregion WHERE name LIKE 'R2 Performance Macroregion %'",
		"DELETE FROM parameterization.catalog_detail WHERE label LIKE 'R2 Performance Detail %'",
		"DELETE FROM parameterization.catalog WHERE name LIKE 'R2 Performance Catalog %'",
		"DELETE FROM parameterization.parameter WHERE key LIKE 'R2_PERFORMANCE_PARAMETER_%'",
	}
	var deleted int64
	for _, statement := range statements {
		result, execErr := tx.Exec(ctx, statement)
		if execErr != nil {
			return fmt.Errorf("cleanup: %w", execErr)
		}
		deleted += result.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cleanup: %w", err)
	}
	fmt.Printf("Cleaned R2 dataset rows: %d\n", deleted)
	return nil
}

func r2Rows(ctx context.Context, conn *pgx.Conn) (int, error) {
	queries := []string{
		"SELECT count(*) FROM institutional_structure.macroregion WHERE name LIKE 'R2 Performance Macroregion %'",
		"SELECT count(*) FROM institutional_structure.municipality WHERE name LIKE 'R2 Performance Municipality %'",
		"SELECT count(*) FROM parameterization.catalog WHERE name LIKE 'R2 Performance Catalog %'",
		"SELECT count(*) FROM parameterization.catalog_detail WHERE label LIKE 'R2 Performance Detail %'",
	}
	total := 0
	for _, query := range queries {
		var count int
		if err := conn.QueryRow(ctx, query).Scan(&count); err != nil {
			return 0, fmt.Errorf("inspect existing dataset: %w", err)
		}
		total += count
	}
	return total, nil
}

func datasetUUID(prefix, index int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", prefix, index+1)
}

func active(index int) bool { return index%10 != 0 }

func macroRows(ids []string) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id, fmt.Sprintf("R2 Performance Macroregion %02d", i+1), fmt.Sprintf("R2M%07d", i+1), active(i)}
	}
	return rows
}
func microRows(ids, parents []string) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id, parents[i%len(parents)], fmt.Sprintf("R2 Performance Microregion %03d", i+1), fmt.Sprintf("R2U%07d", i+1), active(i)}
	}
	return rows
}
func departmentRows(ids, parents []string) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id, parents[i%len(parents)], fmt.Sprintf("R2 Performance Department %03d", i+1), fmt.Sprintf("R2%03d", i+1), active(i)}
	}
	return rows
}
func municipalityRows(ids, parents []string) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id, parents[i%len(parents)], fmt.Sprintf("R2 Performance Municipality %06d", i+1), fmt.Sprintf("R2%06d", i+1), active(i)}
	}
	return rows
}
func centerRows(ids, parents []string) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id, parents[i%len(parents)], fmt.Sprintf("R2C%07d", i+1), fmt.Sprintf("R2 Performance Training Center %05d", i+1), active(i)}
	}
	return rows
}
func unitRows(centerIDs []string, count int) [][]any {
	rows := make([][]any, count)
	for i := range rows {
		rows[i] = []any{datasetUUID(0x27000000, i), centerIDs[i%len(centerIDs)], fmt.Sprintf("R2 Performance Unit %05d", i+1), []string{"ACADEMIC", "ADMINISTRATIVE", "MIXED"}[i%3], active(i)}
	}
	return rows
}
func catalogRows(ids []string) [][]any {
	rows := make([][]any, len(ids))
	for i, id := range ids {
		rows[i] = []any{id, fmt.Sprintf("R2CAT%06d", i+1), fmt.Sprintf("R2 Performance Catalog %04d", i+1), "R2 performance dataset", active(i)}
	}
	return rows
}
func detailRows(catalogIDs []string, count int) [][]any {
	rows := make([][]any, count)
	for i := range rows {
		rows[i] = []any{datasetUUID(0x29000000, i), catalogIDs[i%len(catalogIDs)], fmt.Sprintf("R2D%07d", i+1), fmt.Sprintf("R2 Performance Detail %06d", i+1), i % 1000, active(i)}
	}
	return rows
}
func parameterRows(count int) [][]any {
	rows := make([][]any, count)
	for i := range rows {
		rows[i] = []any{datasetUUID(0x2a000000, i), fmt.Sprintf("R2_PERFORMANCE_PARAMETER_%06d", i+1), strconv.Itoa(i), "string", "R2 performance dataset"}
	}
	return rows
}
