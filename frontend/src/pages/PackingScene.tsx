import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle } from "lucide-react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import type { PackingDimensions, PackingPlacement } from "../types";

type PackingSceneProps = {
  packageDimensions: PackingDimensions;
  placements: PackingPlacement[];
  visibleCount: number;
  resetToken: number;
};

const itemColors = ["#2f7d67", "#d1843f", "#4f78a4", "#a75a67", "#8067a2", "#5d8b45", "#b18832", "#4f858b", "#b05f43", "#627586"];

export function packingColorForSKU(sku: string): string {
  let hash = 0;
  for (let index = 0; index < sku.length; index += 1) hash = ((hash << 5) - hash + sku.charCodeAt(index)) | 0;
  return itemColors[Math.abs(hash) % itemColors.length];
}

let cachedWebGL2Support: boolean | undefined;

function supportsWebGL2(): boolean {
  if (cachedWebGL2Support !== undefined) return cachedWebGL2Support;
  try {
    const canvas = document.createElement("canvas");
    const context = canvas.getContext("webgl2", { failIfMajorPerformanceCaveat: false });
    const available = context !== null;
    context?.getExtension("WEBGL_lose_context")?.loseContext();
    cachedWebGL2Support = available;
    return available;
  } catch {
    cachedWebGL2Support = false;
    return false;
  }
}

function WebGLScene({ packageDimensions, placements, visibleCount, resetToken, onFailure }: PackingSceneProps & { onFailure: () => void }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const groupsRef = useRef<THREE.Group[]>([]);
  const renderRef = useRef<(() => void) | null>(null);
  const cameraRef = useRef<THREE.PerspectiveCamera | null>(null);
  const controlsRef = useRef<OrbitControls | null>(null);
  const frameRef = useRef<number | null>(null);
  const previousVisibleRef = useRef(visibleCount);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, preserveDrawingBuffer: true, powerPreference: "low-power" });
    } catch {
      onFailure();
      return;
    }
    renderer.domElement.dataset.renderer = "webgl2";
    renderer.domElement.setAttribute("aria-label", "包装方案三维视图");
    renderer.domElement.setAttribute("role", "img");
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    renderer.setClearColor(0xf7f9fa, 1);
    host.replaceChildren(renderer.domElement);

    const scene = new THREE.Scene();
    scene.background = new THREE.Color(0xf7f9fa);
    const camera = new THREE.PerspectiveCamera(36, 1, 0.1, 10000);
    cameraRef.current = camera;
    const controls = new OrbitControls(camera, renderer.domElement);
    controlsRef.current = controls;
    controls.enableDamping = false;
    controls.screenSpacePanning = true;
    controls.minPolarAngle = 0.12;
    controls.maxPolarAngle = Math.PI / 2.03;

    const maxDimension = Math.max(packageDimensions.length_cm, packageDimensions.width_cm, packageDimensions.height_cm, 1);
    const center = new THREE.Vector3(packageDimensions.length_cm / 2, packageDimensions.height_cm / 2, packageDimensions.width_cm / 2);
    const resetCamera = () => {
      camera.position.set(
        center.x + maxDimension * 1.55,
        center.y + maxDimension * 1.18,
        center.z + maxDimension * 1.65,
      );
      camera.near = Math.max(maxDimension / 1000, 0.01);
      camera.far = maxDimension * 20;
      camera.updateProjectionMatrix();
      controls.target.copy(center);
      controls.minDistance = maxDimension * 0.55;
      controls.maxDistance = maxDimension * 8;
      controls.update();
    };
    resetCamera();

    scene.add(new THREE.HemisphereLight(0xffffff, 0xb8c0c5, 1.55));
    const keyLight = new THREE.DirectionalLight(0xffffff, 1.35);
    keyLight.position.set(maxDimension * 2, maxDimension * 3, maxDimension * 1.5);
    scene.add(keyLight);
    const fillLight = new THREE.DirectionalLight(0xd8e8f2, 0.7);
    fillLight.position.set(-maxDimension, maxDimension, -maxDimension);
    scene.add(fillLight);

    const floorGeometry = new THREE.BoxGeometry(packageDimensions.length_cm, 0.06, packageDimensions.width_cm);
    const floorMaterial = new THREE.MeshStandardMaterial({ color: 0xe8edef, roughness: 1 });
    const floor = new THREE.Mesh(floorGeometry, floorMaterial);
    floor.position.set(packageDimensions.length_cm / 2, -0.03, packageDimensions.width_cm / 2);
    scene.add(floor);

    const packageGeometry = new THREE.BoxGeometry(packageDimensions.length_cm, packageDimensions.height_cm, packageDimensions.width_cm);
    const packageEdgesGeometry = new THREE.EdgesGeometry(packageGeometry);
    const packageEdgesMaterial = new THREE.LineBasicMaterial({ color: 0x59666d, transparent: true, opacity: 0.72 });
    const packageEdges = new THREE.LineSegments(packageEdgesGeometry, packageEdgesMaterial);
    packageEdges.position.copy(center);
    scene.add(packageEdges);

    const grid = new THREE.GridHelper(Math.max(packageDimensions.length_cm, packageDimensions.width_cm), 10, 0xc5cdd1, 0xdde2e4);
    grid.position.set(packageDimensions.length_cm / 2, 0, packageDimensions.width_cm / 2);
    scene.add(grid);

    const groups = placements.map((placement, index) => {
      const group = new THREE.Group();
      const geometry = new THREE.BoxGeometry(placement.dimensions.length_cm, placement.dimensions.height_cm, placement.dimensions.width_cm);
      const color = new THREE.Color(packingColorForSKU(placement.warehouse_sku));
      const material = new THREE.MeshStandardMaterial({ color, roughness: 0.72, metalness: 0.02, transparent: true, opacity: 0.92 });
      const mesh = new THREE.Mesh(geometry, material);
      const edgeGeometry = new THREE.EdgesGeometry(geometry);
      const edgeMaterial = new THREE.LineBasicMaterial({ color: color.clone().multiplyScalar(0.58) });
      group.add(mesh, new THREE.LineSegments(edgeGeometry, edgeMaterial));
      group.position.set(
        placement.position.x + placement.dimensions.length_cm / 2,
        placement.position.y + placement.dimensions.height_cm / 2,
        placement.position.z + placement.dimensions.width_cm / 2,
      );
      group.visible = index < visibleCount;
      scene.add(group);
      return group;
    });
    groupsRef.current = groups;

    const render = () => renderer.render(scene, camera);
    renderRef.current = render;
    controls.addEventListener("change", render);

    const resize = () => {
      const width = Math.max(host.clientWidth, 1);
      const height = Math.max(host.clientHeight, 1);
      renderer.setSize(width, height, false);
      camera.aspect = width / height;
      camera.updateProjectionMatrix();
      render();
    };
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(host);
    resize();

    const handleContextLost = (event: Event) => {
      event.preventDefault();
      onFailure();
    };
    renderer.domElement.addEventListener("webglcontextlost", handleContextLost);

    return () => {
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
      resizeObserver.disconnect();
      controls.removeEventListener("change", render);
      controls.dispose();
      renderer.domElement.removeEventListener("webglcontextlost", handleContextLost);
      scene.traverse((object) => {
        if (!(object instanceof THREE.Mesh || object instanceof THREE.LineSegments)) return;
        object.geometry?.dispose();
        const materials = Array.isArray(object.material) ? object.material : [object.material];
        materials.forEach((material) => material?.dispose());
      });
      renderer.dispose();
      renderer.forceContextLoss();
      host.replaceChildren();
      groupsRef.current = [];
      renderRef.current = null;
      cameraRef.current = null;
      controlsRef.current = null;
    };
  }, [packageDimensions, placements, onFailure]);

  useEffect(() => {
    const groups = groupsRef.current;
    const previous = previousVisibleRef.current;
    previousVisibleRef.current = visibleCount;
    if (frameRef.current !== null) {
      cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }
    groups.forEach((group, index) => {
      const placement = placements[index];
      group.visible = index < visibleCount;
      if (placement) group.position.y = placement.position.y + placement.dimensions.height_cm / 2;
    });
    const newIndex = visibleCount - 1;
    const placement = placements[newIndex];
    const newGroup = groups[newIndex];
    if (!placement || !newGroup || visibleCount <= previous || window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      renderRef.current?.();
      return;
    }
    const targetY = placement.position.y + placement.dimensions.height_cm / 2;
    const startY = Math.max(packageDimensions.height_cm + placement.dimensions.height_cm, targetY + packageDimensions.height_cm * 0.45);
    const started = performance.now();
    const animate = (now: number) => {
      const progress = Math.min((now - started) / 420, 1);
      const eased = 1 - Math.pow(1 - progress, 3);
      newGroup.position.y = startY + (targetY - startY) * eased;
      renderRef.current?.();
      if (progress < 1) frameRef.current = requestAnimationFrame(animate);
      else frameRef.current = null;
    };
    frameRef.current = requestAnimationFrame(animate);
  }, [packageDimensions.height_cm, placements, visibleCount]);

  useEffect(() => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    const maxDimension = Math.max(packageDimensions.length_cm, packageDimensions.width_cm, packageDimensions.height_cm, 1);
    const center = new THREE.Vector3(packageDimensions.length_cm / 2, packageDimensions.height_cm / 2, packageDimensions.width_cm / 2);
    camera.position.set(center.x + maxDimension * 1.55, center.y + maxDimension * 1.18, center.z + maxDimension * 1.65);
    controls.target.copy(center);
    controls.update();
    renderRef.current?.();
  }, [packageDimensions, resetToken]);

  return <div className="packing-canvas-host" ref={hostRef} />;
}

export default function PackingScene(props: PackingSceneProps) {
  const [webGLFailed, setWebGLFailed] = useState(() => !supportsWebGL2());
  const handleFailure = useCallback(() => setWebGLFailed(true), []);

  if (webGLFailed) return <div className="packing-scene packing-scene-unavailable" data-testid="packing-scene" data-active-renderer="unavailable" role="status">
    <AlertTriangle size={24} />
    <strong>三维视图不可用</strong>
    <span>当前浏览器或设备未启用 WebGL2</span>
  </div>;

  return <div className="packing-scene" data-testid="packing-scene" data-active-renderer="webgl2">
    <WebGLScene {...props} onFailure={handleFailure} />
    <span className="packing-renderer-status">3D · WebGL2</span>
  </div>;
}
