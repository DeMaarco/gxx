// @ts-nocheck

const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
const base = document.body.dataset.base || '/';

const $ = (selector: string, root: ParentNode = document) => root.querySelector(selector);
const $$ = (selector: string, root: ParentNode = document) => Array.from(root.querySelectorAll(selector));

function initPreloader() {
	const preloader = $('[data-preloader]');
	if (!preloader) return;
	const dismiss = () => {
		preloader.classList.add('is-done');
		window.setTimeout(() => preloader.remove(), 700);
	};
	window.addEventListener('load', () => window.setTimeout(dismiss, reduceMotion ? 0 : 520), { once: true });
	window.setTimeout(dismiss, reduceMotion ? 100 : 1600);
}

function initScrollEffects() {
	const progress = $('[data-scroll-progress]');
	const revealItems = $$('.reveal');
	const observer = new IntersectionObserver((entries) => {
		entries.forEach((entry) => {
			if (entry.isIntersecting) {
				entry.target.classList.add('is-visible');
				observer.unobserve(entry.target);
			}
		});
	}, { threshold: 0.12, rootMargin: '0px 0px -8% 0px' });
	revealItems.forEach((item) => observer.observe(item));

	const onScroll = () => {
		const max = document.documentElement.scrollHeight - window.innerHeight;
		if (progress) progress.style.transform = `scaleX(${max > 0 ? window.scrollY / max : 0})`;
		const rail = $('[data-flow-rail]');
		if (rail) {
			const rect = rail.getBoundingClientRect();
			const ratio = Math.min(1, Math.max(0, (window.innerHeight * 0.72 - rect.top) / Math.max(rect.height, 1)));
			const nodes = $$('.flow-node', rail);
			const index = Math.min(nodes.length - 1, Math.floor(ratio * nodes.length));
			nodes.forEach((node, i) => node.classList.toggle('is-active', i <= index));
			const caption = $('[data-flow-caption]');
			const labels = ['INPUT RECEIVED', 'CONTEXT ASSEMBLED', 'MODEL TURN', 'PERMISSION CHECK', 'ACTION TRACE', 'RESULT RETURNED'];
			if (caption) caption.textContent = labels[index] || labels[0];
		}
	};
	window.addEventListener('scroll', onScroll, { passive: true });
	onScroll();
}

function initCursor() {
	const orb = $('.cursor-orb');
	if (!orb || reduceMotion || window.matchMedia('(pointer: coarse)').matches) return;
	let frame = 0;
	window.addEventListener('pointermove', (event) => {
		if (frame) return;
		frame = window.requestAnimationFrame(() => {
			document.documentElement.style.setProperty('--pointer-x', `${event.clientX}px`);
			document.documentElement.style.setProperty('--pointer-y', `${event.clientY}px`);
			frame = 0;
		});
	});
	$$('.magnetic').forEach((button) => {
		button.addEventListener('pointermove', (event) => {
			const rect = button.getBoundingClientRect();
			const x = (event.clientX - rect.left - rect.width / 2) * 0.12;
			const y = (event.clientY - rect.top - rect.height / 2) * 0.12;
			button.style.transform = `translate(${x}px, ${y}px)`;
		});
		button.addEventListener('pointerleave', () => { button.style.transform = ''; });
	});
}

function initCopyButtons() {
	$$('[data-copy]').forEach((button) => {
		button.addEventListener('click', async () => {
			const value = button.getAttribute('data-copy') || '';
			const original = button.getAttribute('data-copy-label') || 'COPY';
			try {
				await navigator.clipboard.writeText(value);
				button.textContent = button.getAttribute('data-copied-label') || 'COPIED';
				button.classList.add('is-copied');
				window.setTimeout(() => { button.textContent = original; button.classList.remove('is-copied'); }, 1600);
			} catch { button.textContent = 'SELECT + COPY'; }
		});
	});
}

function initTerminal() {
	const prompts = ['explain this repository', 'where is the port configured?', 'fix the failing test'];
	const target = $('[data-terminal-prompt]');
	if (!target || reduceMotion) return;
	let promptIndex = 0;
	let charIndex = target.textContent?.length || 0;
	let deleting = true;
	const tick = () => {
		const next = prompts[promptIndex];
		if (deleting) {
			charIndex -= 1;
			if (charIndex <= 0) { deleting = false; promptIndex = (promptIndex + 1) % prompts.length; }
		} else {
			charIndex += 1;
			if (charIndex >= prompts[promptIndex].length) { deleting = true; window.setTimeout(tick, 1800); return; }
		}
		target.textContent = deleting ? next.slice(0, charIndex) : prompts[promptIndex].slice(0, charIndex);
		window.setTimeout(tick, deleting ? 42 : 72);
	};
	window.setTimeout(tick, 2200);
}

function initModes() {
	const copy = {
		ask: ['01', 'Ask before acting.', 'Inspect files, search symbols, read git state, and get an answer without changing the project.', 'READ-ONLY TOOLS'],
		plan: ['02', 'Think before touching.', 'Draft a plan from the workspace, then choose execute, request changes, or cancel.', 'PLAN MENU / READ-ONLY'],
		agent: ['03', 'Let the agent move.', 'Apply patches and run commands according to the current permission policy.', 'FILES + SHELL / POLICY GATED'],
	};
	const visual = $('[data-mode-visual]');
	$$('[data-mode]').forEach((tab) => tab.addEventListener('click', () => {
		const mode = tab.dataset.mode;
		if (!mode || !copy[mode]) return;
		$$('[data-mode]').forEach((item) => { item.classList.toggle('is-active', item === tab); item.setAttribute('aria-selected', item === tab ? 'true' : 'false'); });
		if (visual) { visual.dataset.modeVisual = mode; const core = $('.mode-core b', visual); const annotation = $('.mode-annotation', visual); if (core) core.textContent = mode.toUpperCase(); if (annotation) annotation.innerHTML = mode === 'ask' ? 'WRITE TOOLS / OFF<br />SHELL / OFF' : mode === 'plan' ? 'WRITE TOOLS / OFF<br />SHELL / OFF' : 'WRITE TOOLS / GATED<br />SHELL / GATED'; }
		const [number, title, text, chip] = copy[mode];
		const set = (selector: string, value: string) => { const node = $(selector); if (node) node.textContent = value; };
		set('[data-mode-number]', number); set('[data-mode-title]', title); set('[data-mode-copy]', text); set('[data-mode-chip]', chip);
	}));
}

function initPermissions() {
	const values = {
		ask: ['Preview + confirm changes', 'Preview + approval menu', 'DEFAULT / DENY UNLESS YOU APPROVE'],
		'auto-writes': ['Apply without confirmation', 'Preview + approval menu', 'FILES / APPLY · SHELL / ASK'],
		auto: ['Apply without confirmation', 'Apply without confirmation', 'FILES + SHELL / AUTO'],
	};
	$$('[data-permission]').forEach((tab) => tab.addEventListener('click', () => {
		const key = tab.dataset.permission;
		if (!key || !values[key]) return;
		$$('[data-permission]').forEach((item) => item.classList.toggle('is-active', item === tab));
		const [files, shell, note] = values[key];
		const set = (selector: string, value: string) => { const node = $(selector); if (node) node.textContent = value; };
		set('[data-permission-files]', files); set('[data-permission-shell]', shell); set('[data-permission-note]', note);
	}));
}

function initProviders() {
	const values = {
		openai: ['OpenAI', 'gpt-5.6-sol', 'API key / ChatGPT', 'context · effort · fast', 'OPENAI', 'OPENAI / CODEX'],
		claude: ['Claude', 'claude-sonnet-5', 'Pro/Max / token', 'context · effort · fast', 'CLAUDE', 'ANTHROPIC / CLAUDE'],
	};
	$$('[data-provider]').forEach((tab) => tab.addEventListener('click', () => {
		const key = tab.dataset.provider;
		if (!key || !values[key]) return;
		$$('[data-provider]').forEach((item) => item.classList.toggle('is-active', item === tab));
		const [name, model, login, session, mark, route] = values[key];
		const set = (selector: string, value: string) => { const node = $(selector); if (node) node.textContent = value; };
		set('[data-provider-name]', name); set('[data-provider-model]', model); set('[data-provider-login]', login); set('[data-provider-session]', session); set('[data-provider-mark]', mark); set('[data-provider-route]', route);
		document.documentElement.style.setProperty('--provider-accent', key === 'claude' ? '#c084fc' : '#71efe3');
	}));
}

function initInstallTabs() {
	const values = {
		unix: ['QUICK INSTALL / SHELL', 'curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh', ['THEN', 'cd your-project', '→', 'gxx', '→', 'type what you want']],
		windows: ['QUICK INSTALL / POWERSHELL', 'irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex', ['THEN', 'cd your-project', '→', 'gxx', '→', 'type what you want']],
		source: ['BUILD / GO 1.27+', 'git clone https://github.com/DeMaarco/gxx.git\ncd gxx\ngo install ./cmd/gxx\ngo test ./test/...', ['THEN', 'gxx version', '→', 'gxx', '→', 'type what you want']],
	};
	$$('[data-install]').forEach((tab) => tab.addEventListener('click', () => {
		const key = tab.dataset.install;
		if (!key || !values[key]) return;
		$$('[data-install]').forEach((item) => item.classList.toggle('is-active', item === tab));
		const [label, command, follow] = values[key];
		const labelNode = $('[data-install-label]'); const commandNode = $('[data-install-command]'); const followNode = $('[data-install-follow]'); const copy = $('.copy-button');
		if (labelNode) labelNode.textContent = label;
		if (commandNode) commandNode.textContent = command;
		if (copy) { copy.setAttribute('data-copy', command); copy.textContent = 'COPY'; }
		if (followNode) followNode.innerHTML = follow.map((item, index) => index % 2 ? `<b>${item}</b>` : `<span>${item}</span>`).join('');
	}));
}

function initUseCases() {
	const output = [
		['explain this repository', 'list / search / read / git', 'context assembled from the workspace'],
		['where is the port configured?', 'search / read / path trace', 'answer returned with source context'],
		['fix the failing test', 'read / plan / patch / run', 'permission gate before the change'],
		['add a save button to the form', 'read / plan / edit / verify', 'agent mode follows the policy'],
	];
	const target = $('[data-usecase-output]');
	$$('[data-usecase]').forEach((button) => button.addEventListener('click', () => {
		const index = Number(button.dataset.usecase || 0); const value = output[index];
		$$('[data-usecase]').forEach((item) => item.classList.toggle('is-active', item === button));
		if (target) target.innerHTML = `&gt; ${value[0]}<br /><span>→ ${value[1]}</span><br /><b>${value[2]}</b>`;
	}));
}

function initCharts() {
	$$('.chart-panel, .telemetry-card').forEach((panel) => {
		panel.setAttribute('tabindex', '0');
		panel.setAttribute('role', 'img');
		panel.setAttribute('aria-label', 'Benchmark visualization. No published run data is available yet.');
		const showTooltip = () => {
			panel.classList.add('is-inspected');
			if (!panel.querySelector('.chart-tooltip')) {
				const tooltip = document.createElement('span');
				tooltip.className = 'chart-tooltip mono';
				tooltip.textContent = 'No published run data · awaiting measurement';
				panel.appendChild(tooltip);
			}
		};
		const hideTooltip = () => { panel.classList.remove('is-inspected'); panel.querySelector('.chart-tooltip')?.remove(); };
		panel.addEventListener('focus', showTooltip);
		panel.addEventListener('blur', hideTooltip);
		panel.addEventListener('pointerenter', showTooltip);
		panel.addEventListener('pointerleave', hideTooltip);
	});
}

async function initThree() {
	const mounts = $$('[data-three-scene]');
	if (!mounts.length) return;
	const THREE = await import('three');
	const { GLTFLoader } = await import('three/addons/loaders/GLTFLoader.js');
	const motionScale = reduceMotion ? 0.08 : 1;
	const pointer = new THREE.Vector2(0, 0);
	const scrollPosition = { value: 0 };
	window.addEventListener('pointermove', (event) => { pointer.x = (event.clientX / window.innerWidth) * 2 - 1; pointer.y = -(event.clientY / window.innerHeight) * 2 + 1; }, { passive: true });
	window.addEventListener('scroll', () => { scrollPosition.value = window.scrollY / Math.max(document.documentElement.scrollHeight - window.innerHeight, 1); }, { passive: true });

	const scenes = [];
	const loader = new GLTFLoader();
	let gltfPromise: Promise<any> | null = null;
	const loadAsset = (url: string) => {
		if (!gltfPromise) gltfPromise = new Promise((resolve, reject) => loader.load(url, resolve, undefined, reject));
		return gltfPromise;
	};

	function makeParticles(scene: THREE.Scene, color = 0x71efe3, count = 120) {
		const positions = new Float32Array(count * 3);
		for (let i = 0; i < count; i += 1) { const radius = 1.2 + Math.random() * 2.8; const angle = Math.random() * Math.PI * 2; positions[i * 3] = Math.cos(angle) * radius; positions[i * 3 + 1] = (Math.random() - 0.5) * 2.8; positions[i * 3 + 2] = Math.sin(angle) * radius; }
		const geometry = new THREE.BufferGeometry(); geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));
		const material = new THREE.PointsMaterial({ color, size: 0.018, transparent: true, opacity: 0.72, blending: THREE.AdditiveBlending });
		const particles = new THREE.Points(geometry, material); scene.add(particles); return particles;
	}

	function makeLines(scene: THREE.Scene, color = 0x71efe3, count = 10) {
		const group = new THREE.Group();
		for (let i = 0; i < count; i += 1) { const material = new THREE.LineBasicMaterial({ color, transparent: true, opacity: 0.22 }); const geometry = new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(-2.4, (i - count / 2) * 0.22, -0.2), new THREE.Vector3(0, (i - count / 2) * 0.08, 0.3), new THREE.Vector3(2.4, (i - count / 2) * 0.16, -0.15)]); group.add(new THREE.Line(geometry, material)); }
		scene.add(group); return group;
	}

	function normalize(object: THREE.Object3D) {
		const box = new THREE.Box3().setFromObject(object); const size = box.getSize(new THREE.Vector3()); const max = Math.max(size.x, size.y, size.z) || 1; object.scale.multiplyScalar(2.05 / max); box.setFromObject(object); const center = box.getCenter(new THREE.Vector3()); object.position.sub(center); return object;
	}

	function addAsset(scene: THREE.Scene, url: string, accent = 0x71efe3) {
		loadAsset(url).then((gltf) => { const object = normalize(gltf.scene.clone(true)); object.traverse((child: any) => { if (child.isMesh) { child.castShadow = false; child.receiveShadow = false; if (child.material) { child.material = child.material.clone(); child.material.metalness = 0.72; child.material.roughness = 0.34; } } }); scene.add(object); }).catch(() => {});
		const halo = new THREE.Mesh(new THREE.SphereGeometry(1.04, 24, 24), new THREE.MeshBasicMaterial({ color: accent, transparent: true, opacity: 0.035, blending: THREE.AdditiveBlending })); scene.add(halo); return halo;
	}

	function mount(element: HTMLElement) {
		if (element.dataset.mounted) return;
		try {
			const kind = element.dataset.threeScene || 'hero'; const asset = element.dataset.asset || `${base}models/gxx-core-module.glb`;
			const scene = new THREE.Scene(); const camera = new THREE.PerspectiveCamera(34, 1, 0.1, 100); camera.position.z = kind === 'workspace' ? 6.5 : 6;
			const renderer = new THREE.WebGLRenderer({ alpha: true, antialias: !reduceMotion, powerPreference: 'high-performance' }); renderer.setPixelRatio(Math.min(window.devicePixelRatio, reduceMotion ? 1 : 1.6)); renderer.outputColorSpace = THREE.SRGBColorSpace; renderer.domElement.className = 'three-output'; element.appendChild(renderer.domElement);
			const group = new THREE.Group(); scene.add(group); const accent = kind === 'security' ? 0xff6f86 : kind === 'skills' ? 0xb279ff : 0x71efe3;
			const halo = addAsset(group, asset, accent); const particles = makeParticles(group, accent, kind === 'hero' ? 150 : 90); const lines = makeLines(group, accent, kind === 'skills' ? 14 : 8);
			if (kind === 'workspace') { const grid = new THREE.GridHelper(5.2, 18, 0x71efe3, 0x29404a); grid.position.y = -1.2; (grid.material as any).opacity = 0.25; (grid.material as any).transparent = true; group.add(grid); for (let i = 0; i < 7; i += 1) { const node = new THREE.Mesh(new THREE.BoxGeometry(0.22, 0.22, 0.22), new THREE.MeshBasicMaterial({ color: accent, transparent: true, opacity: 0.8 })); node.position.set((i - 3) * 0.5, -0.4 + (i % 2) * 0.55, Math.sin(i) * 0.6); group.add(node); } }
			if (kind === 'security') { const cage = new THREE.Group(); const material = new THREE.LineBasicMaterial({ color: accent, transparent: true, opacity: 0.7 }); [-1.3, 1.3].forEach((x) => [-1.2, 1.2].forEach((y) => cage.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(x, y, -1.5), new THREE.Vector3(x, y, 1.5)]), material)))); group.add(cage); }
			if (kind === 'skills') { for (let i = 0; i < 5; i += 1) { const node = new THREE.Mesh(new THREE.OctahedronGeometry(0.16, 0), new THREE.MeshBasicMaterial({ color: accent, transparent: true, opacity: 0.9 })); const angle = (i / 5) * Math.PI * 2; node.position.set(Math.cos(angle) * 2, Math.sin(angle * 2) * 0.65, Math.sin(angle) * 2); group.add(node); } }
			const baseCameraZ = camera.position.z;
			const state = { element, scene, camera, renderer, group, halo, particles, lines, kind, baseCameraZ, active: false, time: Math.random() * 10, resize: () => { const rect = element.getBoundingClientRect(); renderer.setSize(Math.max(1, rect.width), Math.max(1, rect.height), false); camera.aspect = Math.max(1, rect.width) / Math.max(1, rect.height); camera.updateProjectionMatrix(); } };
			state.resize(); element.dataset.mounted = 'true'; element.classList.add('has-webgl'); scenes.push(state); return state;
		} catch (error) { element.classList.add('webgl-unavailable'); element.innerHTML = '<span class="webgl-fallback mono">WEBGL FALLBACK / VISUAL SYSTEM ACTIVE</span>'; return null; }
	}

	const observer = new IntersectionObserver((entries) => entries.forEach((entry) => { if (entry.isIntersecting) { const state = mount(entry.target as HTMLElement); if (state) state.active = true; } else { const state = scenes.find((item) => item.element === entry.target); if (state) state.active = false; } }), { rootMargin: '240px' });
	mounts.forEach((mountPoint) => observer.observe(mountPoint));
	window.addEventListener('resize', () => scenes.forEach((state) => state.resize()), { passive: true });
	function animate(now: number) { scenes.forEach((state) => { if (!state.active) return; state.time += 0.006 * motionScale; const t = state.time; state.group.rotation.y += (0.0018 + (pointer.x * 0.0008) + scrollPosition.value * 0.0007) * motionScale; state.group.rotation.x += ((pointer.y * 0.0006) - state.group.rotation.x * 0.002) * motionScale; state.group.rotation.z += scrollPosition.value * 0.00012 * motionScale; state.group.position.y = Math.sin(t * 0.7) * 0.045 * motionScale; state.particles.rotation.y = t * 0.025 * motionScale; state.lines.rotation.z = Math.sin(t * 0.35) * 0.04; state.halo.scale.setScalar(1 + Math.sin(t * 1.2) * 0.05); state.camera.position.x += ((pointer.x * 0.22) - state.camera.position.x) * 0.015 * motionScale; state.camera.position.y += ((pointer.y * 0.12) - state.camera.position.y) * 0.015 * motionScale; const zoom = state.kind === 'hero' ? scrollPosition.value * 0.28 : state.kind === 'skills' ? scrollPosition.value * 0.12 : 0; state.camera.position.z += ((state.baseCameraZ - zoom) - state.camera.position.z) * 0.012 * motionScale; state.camera.lookAt(0, 0, 0); state.renderer.render(state.scene, state.camera); }); window.requestAnimationFrame(animate); }
	window.requestAnimationFrame(animate);
}

initPreloader();
initScrollEffects();
initCursor();
initCopyButtons();
initTerminal();
initModes();
initPermissions();
initProviders();
initInstallTabs();
initUseCases();
initCharts();
initThree();
