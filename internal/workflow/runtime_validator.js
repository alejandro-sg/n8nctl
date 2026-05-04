const fs = require('fs');

const workflowPath = process.argv[1];
const findings = [];
const metadata = loadMetadata();

function finding(severity, code, message, path, node, remediation) {
  findings.push({
    severity,
    code,
    message,
    path: path || '',
    nodeName: node && node.name ? node.name : '',
    nodeId: node && node.id ? String(node.id) : '',
    source: 'n8n-runtime',
    remediation: remediation || '',
  });
}

function loadMetadata() {
  const raw = process.env.N8NCTL_VALIDATOR_METADATA || '{}';
  try {
    return JSON.parse(raw);
  } catch (error) {
    return { nodes: {} };
  }
}

function validateValidatorEnvironment() {
  const expected = metadata.n8nVersion;
  if (!expected) return;

  try {
    const paths = [process.env.N8NCTL_VALIDATOR_NODE_PATH || process.cwd(), process.cwd()];
    const packagePath = require.resolve('n8n/package.json', { paths });
    const pkg = JSON.parse(fs.readFileSync(packagePath, 'utf8'));
    if (pkg.version && pkg.version !== expected) {
      finding(
        'warning',
        'runtime_validator_version_mismatch',
        `n8n validator package version is "${pkg.version}", expected "${expected}"`,
        '',
        null,
        `Install tools/n8n-validator dependencies pinned to n8n ${expected}.`,
      );
    }
  } catch (_) {
    finding(
      'warning',
      'runtime_validator_package_missing',
      `n8n validator package ${expected} is not installed; using embedded metadata checks`,
      '',
      null,
      'Run npm install in tools/n8n-validator to enable package version verification.',
    );
  }
}

function get(obj, path) {
  return path.split('.').reduce((value, key) => value && value[key], obj);
}

function hasValue(value) {
  if (value === undefined || value === null) return false;
  if (typeof value === 'string') return value.trim() !== '';
  if (Array.isArray(value)) return value.length > 0;
  return true;
}

function requiredParam(node, field, label) {
  if (!hasValue(get(node.parameters || {}, field))) {
    finding(
      'error',
      'missing_required_parameter',
      `node "${node.name}" is missing required parameter "${label || field}"`,
      `nodes.${node.name}.parameters.${field}`,
      node,
      `Open "${node.name}" in n8n and complete "${label || field}".`,
    );
  }
}

function requireCredential(node, key, reason) {
  const credentials = node.credentials || {};
  if (!credentials[key] || (!credentials[key].id && !credentials[key].name)) {
    finding(
      'error',
      'missing_required_credential',
      `node "${node.name}" is missing required credential "${key}"`,
      `nodes.${node.name}.credentials.${key}`,
      node,
      reason || `Bind a "${key}" credential to "${node.name}".`,
    );
  }
}

function requireAnyCredential(node, keys, label, reason) {
  const credentials = node.credentials || {};
  const found = keys.some((key) => credentials[key] && (credentials[key].id || credentials[key].name));
  if (!found) {
    finding(
      'error',
      'missing_required_credential',
      `node "${node.name}" is missing required credential "${label}"`,
      `nodes.${node.name}.credentials.${keys.join('|')}`,
      node,
      reason || `Bind one of ${keys.join(', ')} to "${node.name}".`,
    );
  }
}

function validateHttpRequest(node) {
  const params = node.parameters || {};
  const auth = params.authentication || 'none';
  const genericAuthType = params.genericAuthType || '';
  const lowerGenericAuthType = String(genericAuthType).toLowerCase();

  if (auth === 'genericCredentialType' && !hasValue(genericAuthType)) {
    finding(
      'error',
      'missing_required_parameter',
      `node "${node.name}" uses generic credentials but does not select a credential type`,
      `nodes.${node.name}.parameters.genericAuthType`,
      node,
      'Set the HTTP Request authentication mode and credential type.',
    );
  }

  if (auth === 'predefinedCredentialType' && !hasValue(params.nodeCredentialType)) {
    finding(
      'error',
      'missing_required_parameter',
      `node "${node.name}" uses predefined credentials but does not select a credential type`,
      `nodes.${node.name}.parameters.nodeCredentialType`,
      node,
      'Set the HTTP Request predefined credential type.',
    );
  }

  const credentialKeys = Object.keys(node.credentials || {});
  const referencesGoogle = credentialKeys.some((key) => key.toLowerCase().includes('google'));
  if (referencesGoogle && auth !== 'predefinedCredentialType' && auth !== 'genericCredentialType') {
    finding(
      'warning',
      'http_google_auth_mode',
      `HTTP Request node "${node.name}" references Google credentials but auth mode is "${auth}"`,
      `nodes.${node.name}.parameters.authentication`,
      node,
      'Use predefined or generic credential auth for Google APIs, then verify OAuth scopes in the credential.',
    );
  }

  if (referencesGoogle && auth === 'genericCredentialType' && !lowerGenericAuthType.includes('oauth')) {
    finding(
      'warning',
      'http_google_auth_mode',
      `HTTP Request node "${node.name}" references Google credentials without an OAuth generic auth type`,
      `nodes.${node.name}.parameters.genericAuthType`,
      node,
      'Use OAuth2-based generic authentication for Google HTTP requests unless the endpoint expects another scheme.',
    );
  }
}

function validateKnownNode(node) {
  const rule = (metadata.nodes || {})[node.type];
  if (!rule) return;

  for (const param of rule.requiredParameters || []) {
    requiredParam(node, param.path, param.label);
  }

  if (rule.credential) {
    requireCredential(node, rule.credential.key, rule.credential.remediation);
  }

  if (rule.anyCredential) {
    requireAnyCredential(
      node,
      rule.anyCredential.keys || [],
      rule.anyCredential.label || (rule.anyCredential.keys || []).join(' or '),
      rule.anyCredential.remediation,
    );
  }

  if (rule.subworkflow) {
    if (!hasValue(get(node.parameters || {}, 'workflowId.value')) && !hasValue(get(node.parameters || {}, 'workflowId'))) {
      finding(
        'error',
        'missing_subworkflow',
        `node "${node.name}" does not select a child workflow`,
        `nodes.${node.name}.parameters.workflowId`,
        node,
        'Select the child workflow to execute.',
      );
    }
  }

  if (rule.authChecks && rule.authChecks.googleCredentialMode) {
    validateHttpRequest(node);
  }
}

try {
  validateValidatorEnvironment();
  const workflow = JSON.parse(fs.readFileSync(workflowPath, 'utf8'));
  const nodes = Array.isArray(workflow.nodes) ? workflow.nodes : [];
  const nodeNames = new Set(nodes.map((node) => node.name).filter(Boolean));

  for (let i = 0; i < nodes.length; i += 1) {
    const node = nodes[i] || {};
    const path = `nodes[${i}]`;
    if (!hasValue(node.type)) {
      finding('error', 'missing_node_type', `node "${node.name || i}" is missing a node type`, `${path}.type`, node, 'Recreate or repair the node in n8n.');
      continue;
    }
    if (node.typeVersion === undefined || node.typeVersion === null) {
      finding('warning', 'missing_node_type_version', `node "${node.name}" is missing typeVersion`, `${path}.typeVersion`, node, 'Open and save the node in n8n so it records a type version.');
    }
    validateKnownNode(node);
  }

  const connections = workflow.connections || {};
  for (const [source, value] of Object.entries(connections)) {
    if (!nodeNames.has(source)) {
      finding('error', 'connection_from_missing_node', `connections reference missing source node "${source}"`, `connections.${source}`, null, 'Remove stale connections or restore the source node.');
    }
    const encoded = JSON.stringify(value);
    for (const name of encoded.matchAll(/"node"\s*:\s*"([^"]+)"/g)) {
      if (!nodeNames.has(name[1])) {
        finding('error', 'connection_to_missing_node', `connections reference missing target node "${name[1]}"`, `connections.${source}`, null, 'Remove stale connections or restore the target node.');
      }
    }
  }
} catch (error) {
  finding('error', 'runtime_validator_failed', `runtime validator failed: ${error.message}`, '', null, 'Fix the workflow JSON or rerun with --json for details.');
}

process.stdout.write(JSON.stringify({ findings }));
