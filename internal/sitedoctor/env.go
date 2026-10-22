package sitedoctor

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const configFileLimit = 1024 * 1024

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var interpolation = regexp.MustCompile(`\$[A-Za-z_{]`)

// readConfig never follows a linked configuration file or returns its contents
// in an error. The file identity check also rejects replacement during open.
func readConfig(path, prefix string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New(prefix + "_unreadable")
	}
	if info.Size() > configFileLimit {
		return nil, errors.New(prefix + "_too_large")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New(prefix + "_unreadable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, errors.New(prefix + "_unreadable")
	}
	data, err := io.ReadAll(io.LimitReader(f, configFileLimit+1))
	if err != nil {
		return nil, errors.New(prefix + "_unreadable")
	}
	if len(data) > configFileLimit {
		return nil, errors.New(prefix + "_too_large")
	}
	return data, nil
}

// ReadEnvFile reads literal dotenv values without expanding variables or
// loading the project's application. Later definitions take precedence.
func ReadEnvFile(path string) (map[string]string, error) {
	data, err := readConfig(path, "env")
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, raw := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimSpace(line[len("export"):])
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKey.MatchString(key) {
			return nil, errors.New("env_unsupported")
		}
		value = strings.TrimSpace(value)
		if value != "" && (value[0] == '\'' || value[0] == '"') {
			parsed, rest, _, ok := quotedValue(value, false)
			if !ok || (strings.TrimSpace(rest) != "" && !strings.HasPrefix(strings.TrimSpace(rest), "#")) {
				return nil, errors.New("env_unsupported")
			}
			value = parsed
		} else {
			for i, char := range value {
				if char == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
					value = value[:i]
					break
				}
			}
			value = strings.TrimSpace(value)
			switch strings.ToLower(value) {
			case "null", "(null)", "empty", "(empty)":
				value = ""
			}
		}
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("env_unsupported")
		}
		values[key] = value
	}
	return values, nil
}

// quotedValue decodes a single literal. It deliberately rejects multiline and
// unknown PHP double-quoted escape sequences instead of guessing credentials.
func quotedValue(input string, php bool) (string, string, bool, bool) {
	quote := input[0]
	var value strings.Builder
	dynamic := false
	for i := 1; i < len(input); i++ {
		ch := input[i]
		if ch == quote {
			return value.String(), input[i+1:], dynamic, true
		}
		if ch == '\r' || ch == '\n' || ch == 0 {
			return "", "", false, false
		}
		if quote == '"' && ch == '$' && i+1 < len(input) && (input[i+1] == '{' || input[i+1] == '_' || input[i+1] >= 'A' && input[i+1] <= 'Z' || input[i+1] >= 'a' && input[i+1] <= 'z') {
			dynamic = true
		}
		if ch != '\\' || i+1 >= len(input) {
			value.WriteByte(ch)
			continue
		}
		next := input[i+1]
		if next == '\\' || next == quote || quote == '"' && next == '$' {
			value.WriteByte(next)
			i++
			continue
		}
		if quote == '"' {
			switch next {
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			default:
				if php {
					return "", "", false, false
				}
				value.WriteByte(ch)
				continue
			}
			i++
			continue
		}
		value.WriteByte(ch)
	}
	return "", "", false, false
}

// HasCachedConfiguration avoids treating .env as authoritative while Laravel
// serves cached settings. An unreadable cache path also needs manual review.
func HasCachedConfiguration(projectPath string) bool {
	_, err := os.Lstat(filepath.Join(projectPath, "bootstrap", "cache", "config.php"))
	return !os.IsNotExist(err)
}

func LaravelDatabase(projectPath string) (DatabaseConnection, error) {
	// A cached Laravel configuration is the application's connection authority.
	// Its PHP must not be executed, and .env cannot safely select a repair target.
	if HasCachedConfiguration(projectPath) {
		return DatabaseConnection{}, errors.New("database_config_unsupported")
	}
	env, err := ReadEnvFile(filepath.Join(projectPath, ".env"))
	if err != nil {
		return DatabaseConnection{}, err
	}
	if env["DB_SOCKET"] != "" {
		return DatabaseConnection{}, errors.New("database_config_unsupported")
	}
	for key, value := range env {
		if key == "DB_URL" || key == "DATABASE_URL" {
			if value != "" {
				return DatabaseConnection{}, errors.New("database_config_unsupported")
			}
		}
		if strings.HasPrefix(key, "DB_") && interpolation.MatchString(value) {
			return DatabaseConnection{}, errors.New("database_config_unsupported")
		}
	}
	conn := DatabaseConnection{Driver: strings.ToLower(env["DB_CONNECTION"]), Database: env["DB_DATABASE"], Host: env["DB_HOST"], Username: env["DB_USERNAME"], Password: env["DB_PASSWORD"]}
	switch conn.Driver {
	case "mariadb":
		conn.Driver = "mysql"
	case "postgres", "postgresql":
		conn.Driver = "pgsql"
	case "mysql", "pgsql", "sqlite":
	default:
		return DatabaseConnection{}, errors.New("database_config_unsupported")
	}
	if conn.Driver == "sqlite" {
		if conn.Database == "" {
			conn.Database = filepath.Join(projectPath, "database", "database.sqlite")
		} else if conn.Database != ":memory:" && !filepath.IsAbs(conn.Database) {
			conn.Database = filepath.Join(projectPath, conn.Database)
		}
		return conn, nil
	}
	if conn.Database == "" {
		return DatabaseConnection{}, errors.New("database_config_unsupported")
	}
	if _, present := env["DB_HOST"]; !present {
		conn.Host = "127.0.0.1"
	}
	conn.Port = 3306
	if conn.Driver == "pgsql" {
		conn.Port = 5432
	}
	if value, present := env["DB_PORT"]; present {
		if value == "" {
			conn.Port = 0
		} else {
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return DatabaseConnection{}, errors.New("database_port_invalid")
			}
			conn.Port = port
		}
	}
	if _, present := env["DB_USERNAME"]; !present {
		conn.Username = "root"
		if conn.Driver == "pgsql" {
			conn.Username = "postgres"
		}
	}
	return conn, nil
}

type phpConfigToken struct {
	text             string
	literal, dynamic bool
}

// phpConfigTokens is a literal-only lexer, not a PHP interpreter.
func phpConfigTokens(input string) ([]phpConfigToken, bool) {
	tokens := []phpConfigToken{}
	for len(input) > 0 {
		ch := input[0]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			input = input[1:]
			continue
		}
		if strings.HasPrefix(input, "//") || ch == '#' {
			_, input, _ = strings.Cut(input, "\n")
			continue
		}
		if strings.HasPrefix(input, "/*") {
			_, rest, ok := strings.Cut(input[2:], "*/")
			if !ok {
				return nil, false
			}
			input = rest
			continue
		}
		if ch == '\'' || ch == '"' {
			value, rest, dynamic, ok := quotedValue(input, true)
			if !ok {
				return nil, false
			}
			tokens = append(tokens, phpConfigToken{text: value, literal: true, dynamic: dynamic})
			input = rest
			continue
		}
		i := 0
		for i < len(input) && (input[i] >= 'A' && input[i] <= 'Z' || input[i] >= 'a' && input[i] <= 'z' || input[i] >= '0' && input[i] <= '9' || input[i] == '_') {
			i++
		}
		if i == 0 {
			i = 1
		}
		tokens = append(tokens, phpConfigToken{text: input[:i]})
		input = input[i:]
	}
	return tokens, true
}

func WordPressDatabase(projectPath string) (DatabaseConnection, error) {
	data, err := readConfig(filepath.Join(projectPath, "wp-config.php"), "wordpress_config")
	if err != nil {
		return DatabaseConnection{}, err
	}
	tokens, ok := phpConfigTokens(string(data))
	if !ok {
		return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
	}
	values := map[string]string{}
	wanted := map[string]bool{"DB_NAME": true, "DB_USER": true, "DB_PASSWORD": true, "DB_HOST": true}
	braces, parentheses := 0, 0
	for i, token := range tokens {
		if token.literal {
			continue
		}
		// Includes may set constants before wp-config's apparent definitions.
		// WordPress's final wp-settings bootstrap follows all DB definitions and
		// cannot replace the constants that have already been established.
		switch strings.ToLower(token.text) {
		case "include", "include_once", "require", "require_once":
			if len(values) != len(wanted) {
				return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
			}
		}
		switch token.text {
		case "{":
			braces++
		case "}":
			braces--
		case "(":
			parentheses++
		case ")":
			parentheses--
		}
		if braces < 0 || parentheses < 0 {
			return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
		}
		if !strings.EqualFold(token.text, "define") || i+2 >= len(tokens) || tokens[i+1].text != "(" || !tokens[i+2].literal || !wanted[tokens[i+2].text] {
			continue
		}
		// Only a new top-level statement establishes a static database value.
		// Guards without braces and expression calls (ternaries, closures, etc.)
		// are rejected by the preceding-token and nesting checks together.
		openingTag := i == 3 && tokens[0].text == "<" && tokens[1].text == "?" && strings.EqualFold(tokens[2].text, "php")
		statement := i > 0 && tokens[i-1].text == ";"
		if braces != 0 || parentheses != 0 || (!openingTag && !statement) {
			return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
		}
		key := tokens[i+2].text
		_, duplicate := values[key]
		if duplicate || i+5 >= len(tokens) || tokens[i+2].dynamic || tokens[i+3].text != "," || !tokens[i+4].literal || tokens[i+4].dynamic || tokens[i+5].text != ")" {
			return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
		}
		values[key] = tokens[i+4].text
	}
	for key := range wanted {
		if _, exists := values[key]; !exists {
			return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
		}
	}
	conn := DatabaseConnection{Driver: "mysql", Database: values["DB_NAME"], Username: values["DB_USER"], Password: values["DB_PASSWORD"], Host: values["DB_HOST"], Port: 3306}
	if conn.Database == "" || conn.Host == "" || conn.Username == "" {
		return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
	}
	if strings.Contains(conn.Host, ":") && conn.Host != "::1" {
		host, port, err := net.SplitHostPort(conn.Host)
		if err != nil {
			return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
		}
		// Empty ports and socket-shaped host suffixes are ambiguous settings;
		// only a nonempty literal port proves a malformed connection parameter.
		if port == "" || strings.ContainsAny(port, "/\\") {
			return DatabaseConnection{}, errors.New("wordpress_config_unsupported")
		}
		conn.Host = host
		conn.Port, err = strconv.Atoi(port)
		if err != nil || conn.Port < 1 || conn.Port > 65535 {
			return DatabaseConnection{}, errors.New("database_port_invalid")
		}
	}
	return conn, nil
}

// CopyEnvironmentExample creates only an absent .env, preserving exact example
// bytes. Exclusive creation rejects existing files, links and concurrent fixes.
func CopyEnvironmentExample(projectPath string) error {
	data, err := readConfig(filepath.Join(projectPath, ".env.example"), "env_example")
	if err != nil {
		return err
	}
	path := filepath.Join(projectPath, ".env")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return errors.New("env_already_exists")
		}
		return errors.New("env_copy_failed")
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return errors.New("env_copy_failed")
	}
	if err := f.Sync(); err != nil {
		return errors.New("env_copy_failed")
	}
	if err := f.Close(); err != nil {
		return errors.New("env_copy_failed")
	}
	complete = true
	return nil
}
