package sitedoctor

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type DatabaseConnection struct {
	Driver   string `json:"driver"`
	Database string `json:"database"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

type DatabaseState struct {
	Status string `json:"status"`
	Code   string `json:"code"`
}
type DBExecutor func(context.Context, string, []byte) ([]byte, error)

var databaseIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

func databaseSafety(conn DatabaseConnection) string {
	port := 3306
	if conn.Driver == "pgsql" {
		port = 5432
	} else if conn.Driver != "mysql" {
		return "database_unsupported"
	}
	if conn.Port != port || (conn.Host != "127.0.0.1" && !strings.EqualFold(conn.Host, "localhost") && conn.Host != "::1") {
		return "database_external"
	}
	if !databaseIdentifier.MatchString(conn.Database) {
		return "database_name_unsupported"
	}
	name := strings.ToLower(conn.Database)
	if conn.Driver == "mysql" {
		if name == "mysql" || name == "information_schema" || name == "performance_schema" || name == "sys" {
			return "database_name_unsupported"
		}
	} else if name == "postgres" || name == "template0" || name == "template1" {
		return "database_name_unsupported"
	}
	return ""
}

// CanCreateDatabase shares the exact local-target and identifier policy used
// for probing and repair. Project credentials are retained without fallback.
func CanCreateDatabase(conn DatabaseConnection) bool { return databaseSafety(conn) == "" }

const databaseScriptPrelude = `
function result($status, $code) { echo json_encode(['status'=>$status,'code'=>$code]); }
$c = json_decode(stream_get_contents(STDIN), true);
if (!is_array($c) || !isset($c['driver'],$c['database'],$c['host'],$c['username'],$c['password'],$c['port'])) { result('unsupported','database_unsupported'); exit; }
if (!class_exists('PDO') || !in_array($c['driver'], PDO::getAvailableDrivers(), true)) { result('unsupported','database_extension_missing'); exit; }
$host = strtolower($c['host']) === 'localhost' ? '127.0.0.1' : $c['host'];
$options = [PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION, PDO::ATTR_TIMEOUT=>3];
`

const databaseProbeScript = databaseScriptPrelude + `
$phase = 'connect';
try {
    $dsn = $c['driver'] === 'mysql'
        ? 'mysql:host='.$host.';port='.$c['port'].';dbname='.$c['database'].';charset=utf8mb4'
        : 'pgsql:host='.$host.';port='.$c['port'].';dbname='.$c['database'].';connect_timeout=3';
    $pdo = new PDO($dsn, $c['username'], $c['password'], $options);
    $phase = 'query';
    $pdo->query('SELECT 1');
    result('exists','database_ok');
} catch (PDOException $e) {
    $state = (string)$e->getCode(); $native = isset($e->errorInfo[1]) ? (int)$e->errorInfo[1] : 0;
    $pgMissing = $c['driver'] === 'pgsql' && ($state === '3D000' || ($state === '08006' && strpos($e->getMessage(), 'database "'.$c['database'].'" does not exist') !== false));
    $mysqlMissing = $c['driver'] === 'mysql' && $native === 1049;
    $authFailure = $state === '28000' || $state === '28P01' || ($c['driver'] === 'mysql' && $native === 1045);
    $mysqlTransport = $c['driver'] === 'mysql' && (($phase === 'connect' && in_array($native, [2002,2003,2005], true)) || in_array($native, [2006,2013], true));
    $pgsqlTransport = $c['driver'] === 'pgsql' && $phase === 'query' && in_array($state, ['08003','08006'], true);
    if ($phase === 'connect' && ($pgMissing || $mysqlMissing)) result('missing','database_missing');
    elseif ($phase === 'connect' && $authFailure) result('unreachable','database_auth_failed');
    elseif ($mysqlTransport || $pgsqlTransport) result('unreachable','database_unreachable');
    else result('inconclusive','database_probe_failed');
} catch (Throwable $e) { result('inconclusive','database_probe_failed'); }
`

const databaseCreateScript = databaseScriptPrelude + `
try {
    $dsn = $c['driver'] === 'mysql'
        ? 'mysql:host='.$host.';port='.$c['port'].';charset=utf8mb4'
        : 'pgsql:host='.$host.';port='.$c['port'].';dbname=postgres;connect_timeout=3';
    $pdo = new PDO($dsn, $c['username'], $c['password'], $options);
    if ($c['driver'] === 'mysql') {
        $name = str_replace(chr(96), chr(96).chr(96), $c['database']);
        $pdo->exec('CREATE DATABASE IF NOT EXISTS '.chr(96).$name.chr(96).' CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci');
    } else {
        $name = str_replace(chr(34), chr(34).chr(34), $c['database']);
        $pdo->exec('CREATE DATABASE '.chr(34).$name.chr(34));
    }
    result('exists','database_created');
} catch (PDOException $e) {
    $state = (string)$e->getCode(); $native = isset($e->errorInfo[1]) ? (int)$e->errorInfo[1] : 0;
    if ($c['driver'] === 'pgsql' && $state === '42P04') result('exists','database_created');
    elseif ($state === '42501' || $state === '28000' || $state === '28P01' || $native === 1044 || $native === 1045 || $native === 1142) result('unreachable','database_create_denied');
    else result('unreachable','database_create_failed');
} catch (Throwable $e) { result('unreachable','database_create_failed'); }
`

func ProbeDatabase(ctx context.Context, conn DatabaseConnection, executor DBExecutor) (DatabaseState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return DatabaseState{}, err
	}
	if code := databaseSafety(conn); code != "" {
		return DatabaseState{Status: "unsupported", Code: code}, nil
	}
	fallback := DatabaseState{Status: "inconclusive", Code: "database_probe_failed"}
	if executor == nil {
		return fallback, nil
	}
	input, _ := json.Marshal(conn)
	output, err := executor(ctx, databaseProbeScript, input)
	if ctx.Err() != nil {
		return DatabaseState{}, ctx.Err()
	}
	if err != nil || len(output) > 1024 {
		return fallback, nil
	}
	var state DatabaseState
	if json.Unmarshal(output, &state) != nil {
		return fallback, nil
	}
	allowed := map[string]string{
		"database_ok": "exists", "database_missing": "missing",
		"database_auth_failed": "unreachable", "database_unreachable": "unreachable",
		"database_probe_failed": "inconclusive", "database_extension_missing": "unsupported",
		"database_unsupported": "unsupported",
	}
	if status, ok := allowed[state.Code]; !ok || status != state.Status {
		return fallback, nil
	}
	return state, nil
}

func CreateDatabase(ctx context.Context, conn DatabaseConnection, executor DBExecutor) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !CanCreateDatabase(conn) {
		return errors.New("database_config_unsupported")
	}
	if executor == nil {
		return errors.New("database_create_failed")
	}
	input, _ := json.Marshal(conn)
	output, err := executor(ctx, databaseCreateScript, input)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || len(output) > 1024 {
		return errors.New("database_create_failed")
	}
	var state DatabaseState
	if json.Unmarshal(output, &state) == nil {
		if state.Status == "exists" && state.Code == "database_created" {
			return nil
		}
		if state.Status == "unreachable" && state.Code == "database_create_denied" {
			return errors.New("database_create_denied")
		}
		if state.Status == "unsupported" && state.Code == "database_extension_missing" {
			return errors.New("database_extension_missing")
		}
	}
	return errors.New("database_create_failed")
}
