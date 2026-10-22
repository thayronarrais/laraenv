package applicationcontext

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureUnavailableUntilActualEventsAndRejectsMalformed(t *testing.T) {
	root := t.TempDir()
	got, err := ReadCapture(context.Background(), root, "demo", 20)
	if err != nil || got.Status != "unavailable" {
		t.Fatalf("%+v %v", got, err)
	}
	fixtureFile(t, root, "storage/laraenv/context-2026-10-19.jsonl", "{\"v\":1,\"project\":\"other\",\"kind\":\"request\",\"data\":{\"method\":\"PRIVATE\"}}\n{\"v\":1,\"project\":\"demo\",\"kind\":\"request\",\"time\":\"2026-10-19T12:00:00Z\",\"data\":{\"method\":\"GET\",\"status\":500,\"password\":\"PRIVATE\"}}\nnot json\n")
	got, err = ReadCapture(context.Background(), root, "demo", 20)
	if err != nil || got.Status != "partial" || len(got.Events) != 1 || got.Events[0].Data["method"] != "GET" || got.Events[0].Data["password"] != nil {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestIntegrationInstallIsExplicitIdempotentAndRefusesExistingFiles(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "artisan", "<?php")
	if _, err := InstallIntegration(root, "demo", false); err == nil {
		t.Fatal("unconfirmed install accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "app")); !os.IsNotExist(err) {
		t.Fatal("unconfirmed install wrote files")
	}
	if _, err := InstallIntegration(root, "demo", true); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallIntegration(root, "demo", true); err != nil {
		t.Fatal("idempotent installation failed", err)
	}
	fixtureFile(t, root, "app/Providers/LaraEnvContextServiceProvider.php", "USER_EDIT")
	if _, err := InstallIntegration(root, "demo", true); err == nil {
		t.Fatal("user integration overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "bootstrap", "providers.php")); !os.IsNotExist(err) {
		t.Fatal("provider registered implicitly")
	}
	fixtureFile(t, root, ".ai/guidelines/laraenv.md", "USER_GUIDELINE")
	if _, err := WriteGuidelines(root, "demo", true); err == nil {
		t.Fatal("user guideline overwritten")
	}
}

func TestGeneratedProviderEmitsEventsOnlyAfterOptIn(t *testing.T) {
	php := fixturePHP(t)
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, "artisan", "<?php")
			if err := os.MkdirAll(filepath.Join(root, "storage"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := InstallIntegration(root, "demo", true); err != nil {
				t.Fatal(err)
			}
			fixtureFile(t, root, "fixture.php", phpEventFixture)
			cmd := exec.Command(php, filepath.Join(root, "fixture.php"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "CAPTURE_FIXTURE_ENABLED="+map[bool]string{false: "0", true: "1"}[enabled])
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("PHP fixture: %v\n%s", err, output)
			}
			got, err := ReadCapture(context.Background(), root, "demo", 50)
			if err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if got.Status != "unavailable" {
					t.Fatalf("disabled provider captured: %+v", got)
				}
				return
			}
			kinds := map[string]bool{}
			for _, event := range got.Events {
				kinds[event.Kind] = true
			}
			for _, kind := range []string{"request", "query", "job", "view", "log", "dump"} {
				if !kinds[kind] {
					t.Errorf("missing emitted %s in %+v", kind, got)
				}
			}
			if len(got.Traces) != 4 {
				t.Fatalf("expected request plus three independent job traces: %+v", got)
			}
			repeated := false
			for _, trace := range got.Traces {
				if trace.Queries == 2 && trace.RepeatedQueries == 1 {
					repeated = true
				}
			}
			if !repeated {
				t.Fatalf("repeated-query grouping missing: %+v", got.Traces)
			}
			for _, event := range got.Events {
				if len(event.TraceID) != 32 {
					t.Fatalf("event lacks scoped trace: %+v", event)
				}
			}
			requestTrace := ""
			for _, event := range got.Events {
				if event.Kind == "request" {
					requestTrace = event.TraceID
				}
			}
			for _, event := range got.Events {
				if event.Kind == "dump" && event.TraceID != requestTrace {
					t.Fatal("failed job trace leaked into later root activity")
				}
			}
			second := exec.Command(php, filepath.Join(root, "fixture.php"))
			second.Dir = root
			second.Env = cmd.Env
			if output, err := second.CombinedOutput(); err != nil {
				t.Fatalf("second PHP run: %v %s", err, output)
			}
			again, err := ReadCapture(context.Background(), root, "demo", 200)
			if err != nil || len(again.Traces) != 8 {
				t.Fatalf("separate executions shared trace IDs: %+v %v", again, err)
			}
			for _, event := range got.Events {
				for _, value := range event.Data {
					if text, ok := value.(string); ok && strings.Contains(text, "PRIVATE") {
						t.Fatal("provider captured private values")
					}
				}
			}
		})
	}
}

func fixturePHP(t *testing.T) string {
	t.Helper()
	php, err := exec.LookPath("php")
	if err != nil {
		php = `C:\laraenv\php\8.3\php.exe`
		if _, err = os.Stat(php); err != nil {
			t.Skip("PHP unavailable; set PATH to run generated provider fixture")
		}
	}
	return php
}

func TestProviderRefusesProductionAndStopsAtDailyCap(t *testing.T) {
	php := fixturePHP(t)
	for _, mode := range []string{"production", "capped"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, "artisan", "<?php")
			fixtureFile(t, root, "fixture.php", phpEventFixture)
			if err := os.MkdirAll(filepath.Join(root, "storage"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := InstallIntegration(root, "demo", true); err != nil {
				t.Fatal(err)
			}
			name := "storage/laraenv/context-" + time.Now().UTC().Format("2006-01-02") + ".jsonl"
			if mode == "capped" {
				fixtureFile(t, root, name, strings.Repeat("x", 2<<20))
				fixtureFile(t, root, "storage/laraenv/context-0000-aa-bb.jsonl", "USER_FILE")
			}
			cmd := exec.Command(php, filepath.Join(root, "fixture.php"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "CAPTURE_FIXTURE_ENABLED=1", "CAPTURE_FIXTURE_ENVIRONMENT="+map[string]string{"production": "production", "capped": "local"}[mode])
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("PHP gate/cap: %v %s", err, output)
			}
			if mode == "production" {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(err) {
					t.Fatal("production capture was created")
				}
			} else {
				st, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
				if err != nil || st.Size() != 2<<20 {
					t.Fatalf("daily cap changed: %v %v", st, err)
				}
				if data, err := os.ReadFile(filepath.Join(root, "storage", "laraenv", "context-0000-aa-bb.jsonl")); err != nil || string(data) != "USER_FILE" {
					t.Fatal("retention deleted unrelated file", err)
				}
			}
		})
	}
}

const phpEventFixture = `<?php
namespace Illuminate\Support { class ServiceProvider { protected $app; public function __construct($app) {$this->app=$app;} } }
namespace Symfony\Component\VarDumper { class VarDumper { public static $handler; public static function setHandler($h) {$old=self::$handler;self::$handler=$h;return $old;} public static function dump($v,?string $label=null):mixed {if(self::$handler){return (self::$handler)($v,$label);}return null;} } }
namespace {
function config($key,$default=null) { $map=['laraenv-context.enabled'=>getenv('CAPTURE_FIXTURE_ENABLED')==='1','laraenv-context.project'=>'demo','laraenv-context.mail_tag'=>'fixture-tag'];return $map[$key]??$default; }
function storage_path($path='') {return __DIR__.'/storage/'.$path;}
class FixtureEvents { public $listeners=[];public function listen($event,$callback){$this->listeners[$event][]=$callback;} public function emit($event,$payload){foreach($this->listeners[$event]??[] as $callback){$callback($payload);}} }
class FixtureView { public $callback;public function composer($pattern,$callback){$this->callback=$callback;} }
class FixtureApp implements \ArrayAccess {public $events;public $view;function __construct(){$this->events=new FixtureEvents;$this->view=new FixtureView;} function environment($e){return $e===(getenv('CAPTURE_FIXTURE_ENVIRONMENT')?:'local');}function runningInConsole(){return true;}function terminating($f){}function offsetExists($k):bool{return true;}function offsetGet($k):mixed{return $k==='events'?$this->events:$this->view;}function offsetSet($k,$v):void{}function offsetUnset($k):void{} }
class FixtureHeaders {public $values=['X-Tags'=>'existing','X-LaraEnv-Project'=>'stale-project'];function remove($name){unset($this->values[$name]);}function addTextHeader($name,$value){$this->values[$name]=$value;}function get($name){if(!isset($this->values[$name])){return null;}return new class($this->values[$name]) {private $value;function __construct($v){$this->value=$v;}function getBodyAsString(){return $this->value;}};} }
require __DIR__.'/app/Providers/LaraEnvContextServiceProvider.php';
\Symfony\Component\VarDumper\VarDumper::$handler=function($value,?string $label=null):mixed {$GLOBALS['fixturePreviousDumper']=true;$GLOBALS['fixturePreviousLabel']=$label;return $value;};
$app=new FixtureApp;(new \App\Providers\LaraEnvContextServiceProvider($app))->boot();
$headers=new FixtureHeaders;
$message=new class($headers) {private $headers;function __construct($h){$this->headers=$h;}function getHeaders(){return $this->headers;}};
$app->events->emit('Illuminate\Mail\Events\MessageSending',(object)['message'=>$message]);
$enabled=getenv('CAPTURE_FIXTURE_ENABLED')==='1' && (getenv('CAPTURE_FIXTURE_ENVIRONMENT')?:'local')==='local';
if($enabled && (($headers->values['X-LaraEnv-Project']??'')!=='demo' || ($headers->values['X-Tags']??'')!=='existing, fixture-tag')){throw new \RuntimeException('mail attribution failed or original tags lost');}
if(!$enabled && ($headers->values['X-LaraEnv-Project']??'')!=='stale-project'){throw new \RuntimeException('disabled provider altered mail');}
$request=new class {function method(){return 'GET';}function route(){return new class {function uri(){return 'users/{user}';}};}};
$response=new class {function getStatusCode(){return 500;}};
$app->events->emit('Illuminate\Foundation\Http\Events\RequestHandled',(object)['request'=>$request,'response'=>$response]);
$app->events->emit('Illuminate\Database\Events\QueryExecuted',(object)['sql'=>"select * from users where email='PRIVATE'",'bindings'=>['PRIVATE'],'time'=>1.5,'connectionName'=>'mysql']);
$app->events->emit('Illuminate\Database\Events\QueryExecuted',(object)['sql'=>"select * from users where email='PRIVATE'",'bindings'=>['PRIVATE'],'time'=>1.5,'connectionName'=>'mysql']);
$job=new class {function resolveName(){return 'App\Jobs\Welcome';}};
$app->events->emit('Illuminate\Queue\Events\JobProcessing',(object)['job'=>$job]);
$app->events->emit('Illuminate\Database\Events\QueryExecuted',(object)['sql'=>'select count(*) from jobs','bindings'=>[],'time'=>2,'connectionName'=>'mysql']);
$app->events->emit('Illuminate\Queue\Events\JobProcessed',(object)['job'=>$job]);
$app->events->emit('Illuminate\Queue\Events\JobProcessing',(object)['job'=>$job]);
$app->events->emit('Illuminate\Database\Events\QueryExecuted',(object)['sql'=>'select count(*) from jobs','bindings'=>[],'time'=>2,'connectionName'=>'mysql']);
$app->events->emit('Illuminate\Queue\Events\JobFailed',(object)['job'=>$job]);
$app->events->emit('Illuminate\Queue\Events\JobProcessing',(object)['job'=>$job]);
$app->events->emit('Illuminate\Database\Events\QueryExecuted',(object)['sql'=>'select count(*) from jobs','bindings'=>[],'time'=>2,'connectionName'=>'mysql']);
$app->events->emit('Illuminate\Queue\Events\JobExceptionOccurred',(object)['job'=>$job]);
if($app->view->callback){($app->view->callback)(new class {function name(){return 'welcome';}});}
$app->events->emit('Illuminate\Log\Events\MessageLogged',(object)['level'=>'error','message'=>'PRIVATE','context'=>['password'=>'PRIVATE']]);
$dumpValue=['PRIVATE'=>'PRIVATE'];
if(\Symfony\Component\VarDumper\VarDumper::dump($dumpValue,'fixture-label')!==$dumpValue || ($GLOBALS['fixturePreviousLabel']??null)!=='fixture-label'){throw new \RuntimeException('previous dumper label or return value changed');}
$countable=new class implements \Countable {function count():int {throw new \RuntimeException('collector invoked user Countable code');}};
if(\Symfony\Component\VarDumper\VarDumper::dump($countable)!==$countable){throw new \RuntimeException('previous dumper return object changed');}
if(empty($GLOBALS['fixturePreviousDumper'])){throw new \RuntimeException('previous dumper was not preserved');}
}
`
