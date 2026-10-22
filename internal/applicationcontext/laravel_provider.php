<?php

namespace App\Providers;

use Illuminate\Support\ServiceProvider;

/** Local, explicitly enabled metadata collector. No HTTP routes or dependencies. */
class LaraEnvContextServiceProvider extends ServiceProvider
{
    private bool $writing = false;
    private string $traceId = '';
    private array $traceStack = [];

    public function boot(): void
    {
        if (!$this->app->environment('local') || !config('laraenv-context.enabled', false)) {
            return;
        }
        try { $this->traceId = bin2hex(random_bytes(16)); }
        catch (\Throwable $ignored) { return; }

        $events = $this->app['events'];
        $events->listen('Illuminate\Foundation\Http\Events\RequestHandled', function ($event): void {
            $route = $event->request->route();
            $this->record('request', [
                'method' => $event->request->method(),
                'route' => is_object($route) && method_exists($route, 'uri') ? $route->uri() : '[unmatched]',
                'status' => $event->response->getStatusCode(),
                'durationMs' => max(0, round((microtime(true) - ($_SERVER['REQUEST_TIME_FLOAT'] ?? microtime(true))) * 1000, 2)),
            ]);
        });
        $events->listen('Illuminate\Database\Events\QueryExecuted', function ($event): void {
            // Store only the operation keyword; SQL and bindings can contain secrets.
            preg_match('/^\s*(select|insert|update|delete|with|create|alter|drop|begin|commit|rollback)\b/i', $event->sql, $match);
            $this->record('query', ['operation' => strtolower($match[1] ?? 'other'), 'fingerprint' => hash('sha256', $event->sql), 'durationMs' => $event->time, 'connection' => $event->connectionName]);
        });
        foreach (['JobProcessing' => 'processing', 'JobProcessed' => 'processed', 'JobFailed' => 'failed', 'JobExceptionOccurred' => 'exception'] as $name => $state) {
            $events->listen('Illuminate\\Queue\\Events\\'.$name, function ($event) use ($state): void {
                $jobId = spl_object_id($event->job);
                if ($state === 'processing') {
                    try { $nextTrace = bin2hex(random_bytes(16)); }
                    catch (\Throwable $ignored) { return; }
                    $this->traceStack[] = ['job' => $jobId, 'trace' => $this->traceId];
                    $this->traceId = $nextTrace;
                } elseif (!$this->traceStack || end($this->traceStack)['job'] !== $jobId) {
                    // A final-failure event can follow an exception event for the
                    // same attempt. Never restore/pop another job's context.
                    return;
                }
                $this->record('job', ['class' => $event->job->resolveName(), 'state' => $state]);
                if ($state !== 'processing') { $this->traceId = array_pop($this->traceStack)['trace']; }
            });
        }
        $this->app['view']->composer('*', function ($view): void {
            $this->record('view', ['name' => $view->name()]);
        });
        $events->listen('Illuminate\Log\Events\MessageLogged', function ($event): void {
            $this->record('log', ['level' => $event->level]);
        });
        $events->listen('Illuminate\Mail\Events\MessageSending', function ($event): void {
            $headers = $event->message->getHeaders();
            $headers->remove('X-LaraEnv-Project');
            $headers->addTextHeader('X-LaraEnv-Project', (string) config('laraenv-context.project'));
            // Mailpit's documented X-Tags support creates searchable attribution.
            $existing = $headers->get('X-Tags');
            $tags = $existing ? $existing->getBodyAsString().', ' : '';
            $headers->remove('X-Tags');
            $headers->addTextHeader('X-Tags', $tags.(string) config('laraenv-context.mail_tag'));
        });

        if (class_exists(\Symfony\Component\VarDumper\VarDumper::class)) {
            $previous = null;
            $handler = function ($value, ?string $label = null) use (&$previous, &$handler): mixed {
                // Counting Countable objects would execute user-defined methods.
                $this->record('dump', ['type' => get_debug_type($value), 'count' => is_array($value) ? count($value) : 1]);
                if ($previous) {
                    return $previous($value, $label);
                }
                // Preserve the provider's default dumper when none was installed.
                \Symfony\Component\VarDumper\VarDumper::setHandler(null);
                try { return \Symfony\Component\VarDumper\VarDumper::dump($value, $label); }
                finally { \Symfony\Component\VarDumper\VarDumper::setHandler($handler); }
            };
            $previous = \Symfony\Component\VarDumper\VarDumper::setHandler($handler);
        }
    }

    private function record(string $kind, array $data): void
    {
        if ($this->writing) { return; }
        $this->writing = true;
        try {
            $storage = realpath(storage_path());
            if ($storage === false) { return; }
            $directory = $storage.DIRECTORY_SEPARATOR.'laraenv';
            if (is_link($directory)) { return; }
            if (!is_dir($directory) && !@mkdir($directory, 0700, true) && !is_dir($directory)) { return; }
            $path = $directory.DIRECTORY_SEPARATOR.'context-'.gmdate('Y-m-d').'.jsonl';
            if (is_link($path)) { return; }
            foreach (glob($directory.DIRECTORY_SEPARATOR.'context-????-??-??.jsonl') ?: [] as $old) {
                $name = basename($old);
                if (preg_match('/^context-(\d{4})-(\d{2})-(\d{2})\.jsonl$/D', $name, $date)
                    && checkdate((int) $date[2], (int) $date[3], (int) $date[1])
                    && !is_link($old) && is_file($old)
                    && $name < 'context-'.gmdate('Y-m-d', time() - 6 * 86400).'.jsonl') { @unlink($old); }
            }
            foreach ($data as &$value) {
                if (is_string($value)) { $value = substr(preg_replace('/[\x00-\x1F\x7F]/', '', $value), 0, 256); }
            }
            unset($value);
            $observed = (new \DateTimeImmutable('now', new \DateTimeZone('UTC')))->format('Y-m-d\TH:i:s.u\Z');
            $line = json_encode(['v' => 1, 'project' => (string) config('laraenv-context.project'), 'traceId' => $this->traceId, 'time' => $observed, 'kind' => $kind, 'data' => $data], JSON_INVALID_UTF8_SUBSTITUTE | JSON_UNESCAPED_SLASHES);
            if ($line === false || strlen($line) > 8190) { return; }
            $file = @fopen($path, 'ab');
            if ($file === false) { return; }
            try {
                if (!flock($file, LOCK_EX)) { return; }
                $stat = fstat($file);
                if ($stat && $stat['size'] + strlen($line) + 1 <= 2 * 1024 * 1024) { fwrite($file, $line."\n"); }
                flock($file, LOCK_UN);
            } finally { fclose($file); }
        } catch (\Throwable $ignored) {
            // Diagnostic capture must never break the application or log itself.
        } finally { $this->writing = false; }
    }
}
