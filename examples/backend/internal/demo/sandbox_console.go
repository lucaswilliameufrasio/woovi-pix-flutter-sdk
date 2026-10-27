package demo

import "net/http"

func (s *Server) sandboxConsole(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sandboxConsoleHTML))
}

const sandboxConsoleHTML = `<!doctype html>
<html lang="pt-BR">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Woovi Sandbox — console de teste</title>
<style>
:root{color-scheme:light dark;font:16px system-ui,sans-serif}body{max-width:760px;margin:2rem auto;padding:0 1rem;line-height:1.5}h1{line-height:1.15}section{border:1px solid #8888;border-radius:12px;padding:1rem;margin:1rem 0}label{display:block;margin:.75rem 0 .25rem}input{box-sizing:border-box;width:100%;padding:.7rem;font:inherit}button{padding:.65rem .85rem;margin:.7rem .4rem .2rem 0;font:inherit;cursor:pointer}button.primary{font-weight:700}small,.muted{opacity:.8}#result{white-space:pre-wrap;overflow-wrap:anywhere;min-height:2rem} .warning{border-left:4px solid #d98b00;padding-left:.8rem}
</style>
<main>
<h1>Woovi Sandbox — console de teste</h1>
<p class="warning"><strong>Somente ambiente de teste.</strong> O botão de sucesso chama a operação oficial de pagamento de teste da Woovi para a cobrança sandbox deste checkout. Os cenários locais não alteram a Woovi nem o ledger persistido; eles substituem temporariamente as respostas de status deste processo.</p>
<section>
<h2>Conectar ao checkout do app</h2>
<p>Use o mesmo token de sessão e a mesma chave de idempotência passados ao app Flutter. O AppID nunca é solicitado nem enviado ao navegador.</p>
<label for="session-token">Token de sessão sandbox</label><input id="session-token" type="password" autocomplete="off">
<label for="order-id">Pedido da rodada atual</label><input id="order-id" autocomplete="off" value="sandbox-order-1">
<label for="idempotency-key">Chave de idempotência do app</label><input id="idempotency-key" autocomplete="off">
<button id="connect" class="primary">Localizar ou criar checkout de teste</button>
<p id="checkout"></p>
</section>
<section>
<h2>Simulações</h2>
<p>A simulação local dura até o backend reiniciar. Para observar o app, mantenha o Flutter aberto no mesmo checkout.</p>
<button id="woovi-paid" class="primary" disabled>Solicitar pagamento de sucesso na Woovi sandbox</button>
<button data-scenario="paid" disabled>Local: pago</button>
<button data-scenario="expired" disabled>Local: expirado</button>
<button data-scenario="late-paid" disabled>Local: expirado e depois pago</button>
<p id="result" role="status" aria-live="polite"></p>
</section>
<p class="muted">Não exponha este backend publicamente. Os cenários locais testam o comportamento do cliente, não a confirmação financeira do PSP. Para validar webhooks e demais respostas reais, use fixtures locais nos testes ou os recursos suportados pela conta sandbox.</p>
</main>
<script>
const sessionTokenInput = document.querySelector('#session-token');
const orderIdInput = document.querySelector('#order-id');
const idempotencyKeyInput = document.querySelector('#idempotency-key');
const checkoutLabel = document.querySelector('#checkout');
const resultLabel = document.querySelector('#result');
const actionButtons = [...document.querySelectorAll('[data-scenario]'), document.querySelector('#woovi-paid')];
let checkoutSession;

function idempotencyKey() {
  const storageKey = 'wooviSandboxIdempotencyKey';
  let key = sessionStorage.getItem(storageKey);
  if (!key) {
    key = crypto.randomUUID();
    sessionStorage.setItem(storageKey, key);
  }
  return key;
}

idempotencyKeyInput.value = idempotencyKey();

async function connectCheckout() {
  const sessionToken = sessionTokenInput.value.trim();
  const orderId = orderIdInput.value.trim();
  const requestKey = idempotencyKeyInput.value.trim();
  if (sessionToken.length < 32 || orderId.length < 1 || requestKey.length < 8) {
    resultLabel.textContent = 'Informe o token sandbox, o pedido e uma chave de idempotência válida.';
    return;
  }
  resultLabel.textContent = 'Conectando…';
  try {
    const response = await fetch('/v1/merchant/checkout-sessions', {
      method: 'POST',
      headers: {'Content-Type': 'application/json', 'X-Sandbox-Session': sessionToken, 'Idempotency-Key': requestKey},
      body: JSON.stringify({order_id: orderId})
    });
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.message || 'HTTP ' + response.status);
    }
    checkoutSession = body;
    checkoutLabel.textContent = 'Checkout ' + body.checkout_id + ' — estado inicial: ' + body.status + '. Use a mesma chave no app Flutter.';
    for (const button of actionButtons) {
      button.disabled = false;
    }
    resultLabel.textContent = 'Checkout pronto. Escolha uma simulação.';
  } catch (error) {
    resultLabel.textContent = 'Falha ao conectar: ' + error.message;
  }
}

async function runScenario(scenario) {
  if (!checkoutSession) {
    return;
  }
  resultLabel.textContent = 'Executando simulação…';
  try {
    const response = await fetch('/v1/sandbox/test-scenarios/' + encodeURIComponent(scenario) + '?checkout_id=' + encodeURIComponent(checkoutSession.checkout_id), {
      method: 'POST',
      headers: {'Authorization': 'Bearer ' + checkoutSession.access_token}
    });
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.message || 'HTTP ' + response.status);
    }
    resultLabel.textContent = 'Cenário local ativo: ' + body.scenario + '. O estado mostrado pelo Flutter é sintético e não foi gravado como pagamento.';
  } catch (error) {
    resultLabel.textContent = 'Falha na simulação local: ' + error.message;
  }
}

async function simulateWooviPayment() {
  if (!checkoutSession) {
    return;
  }
  resultLabel.textContent = 'Solicitando pagamento de teste à Woovi…';
  try {
    const response = await fetch('/v1/sandbox/test-pay?checkout_id=' + encodeURIComponent(checkoutSession.checkout_id), {
      method: 'POST',
      headers: {'Authorization': 'Bearer ' + checkoutSession.access_token}
    });
    const body = await response.json();
    if (!response.ok) {
      throw new Error(body.message || 'HTTP ' + response.status);
    }
    resultLabel.textContent = 'Pagamento de teste solicitado à Woovi. O app deve confirmar quando o backend observar o novo estado via GET.';
  } catch (error) {
    resultLabel.textContent = 'Falha ao solicitar pagamento sandbox: ' + error.message;
  }
}

document.querySelector('#connect').addEventListener('click', connectCheckout);
document.querySelector('#woovi-paid').addEventListener('click', simulateWooviPayment);
for (const button of document.querySelectorAll('[data-scenario]')) {
  button.addEventListener('click', () => runScenario(button.dataset.scenario));
}
</script>
</html>`
