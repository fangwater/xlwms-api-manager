import { useCallback, useEffect, useRef, useState } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import type { PackingCartonSpec, PackingPlacement } from "../types";

export type PackingRendererMode = "auto" | "2d";

type PackingSceneProps = {
  carton: PackingCartonSpec;
  placements: PackingPlacement[];
  visibleCount: number;
  rendererMode: PackingRendererMode;
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

function WebGLScene({ carton, placements, visibleCount, resetToken, onFailure }: PackingSceneProps & { onFailure: () => void }) {
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
    renderer.domElement.setAttribute("aria-label", "装箱方案三维视图");
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

    const maxDimension = Math.max(carton.length_cm, carton.width_cm, carton.height_cm, 1);
    const center = new THREE.Vector3(carton.length_cm / 2, carton.height_cm / 2, carton.width_cm / 2);
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

    const floorGeometry = new THREE.PlaneGeometry(carton.length_cm, carton.width_cm);
    const floorMaterial = new THREE.MeshStandardMaterial({ color: 0xe8edef, roughness: 1, side: THREE.DoubleSide });
    const floor = new THREE.Mesh(floorGeometry, floorMaterial);
    floor.rotation.x = -Math.PI / 2;
    floor.position.set(carton.length_cm / 2, -0.03, carton.width_cm / 2);
    scene.add(floor);

    const cartonGeometry = new THREE.BoxGeometry(carton.length_cm, carton.height_cm, carton.width_cm);
    const cartonEdgesGeometry = new THREE.EdgesGeometry(cartonGeometry);
    const cartonEdgesMaterial = new THREE.LineBasicMaterial({ color: 0x59666d, transparent: true, opacity: 0.72 });
    const cartonEdges = new THREE.LineSegments(cartonEdgesGeometry, cartonEdgesMaterial);
    cartonEdges.position.copy(center);
    scene.add(cartonEdges);

    const grid = new THREE.GridHelper(Math.max(carton.length_cm, carton.width_cm), 10, 0xc5cdd1, 0xdde2e4);
    grid.position.set(carton.length_cm / 2, 0, carton.width_cm / 2);
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
  }, [carton, placements, onFailure]);

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
    const startY = Math.max(carton.height_cm + placement.dimensions.height_cm, targetY + carton.height_cm * 0.45);
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
  }, [carton.height_cm, placements, visibleCount]);

  useEffect(() => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    const maxDimension = Math.max(carton.length_cm, carton.width_cm, carton.height_cm, 1);
    const center = new THREE.Vector3(carton.length_cm / 2, carton.height_cm / 2, carton.width_cm / 2);
    camera.position.set(center.x + maxDimension * 1.55, center.y + maxDimension * 1.18, center.z + maxDimension * 1.65);
    controls.target.copy(center);
    controls.update();
    renderRef.current?.();
  }, [carton, resetToken]);

  return <div className="packing-canvas-host" ref={hostRef} />;
}

type Point = { x: number; y: number };

function CanvasScene({ carton, placements, visibleCount }: Pick<PackingSceneProps, "carton" | "placements" | "visibleCount">) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const draw = () => {
      const width = Math.max(canvas.clientWidth, 1);
      const height = Math.max(canvas.clientHeight, 1);
      const ratio = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(width * ratio);
      canvas.height = Math.round(height * ratio);
      const context = canvas.getContext("2d");
      if (!context) return;
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
      context.clearRect(0, 0, width, height);
      context.fillStyle = "#f7f9fa";
      context.fillRect(0, 0, width, height);

      const cosine = Math.cos(Math.PI / 6);
      const sine = Math.sin(Math.PI / 6);
      const availableWidth = Math.max(width - 58, 1);
      const availableHeight = Math.max(height - 52, 1);
      const scale = Math.max(Math.min(
        availableWidth / ((carton.length_cm + carton.width_cm) * cosine),
        availableHeight / (carton.height_cm + (carton.length_cm + carton.width_cm) * sine),
      ), 0.01);
      const totalWidth = (carton.length_cm + carton.width_cm) * cosine * scale;
      const totalHeight = (carton.height_cm + (carton.length_cm + carton.width_cm) * sine) * scale;
      const originX = (width - totalWidth) / 2 + carton.width_cm * cosine * scale;
      const originY = (height - totalHeight) / 2 + carton.height_cm * scale;
      const project = (x: number, y: number, z: number): Point => ({
        x: originX + (x - z) * cosine * scale,
        y: originY - y * scale + (x + z) * sine * scale,
      });
      const polygon = (points: Point[], fill: string, stroke: string) => {
        context.beginPath();
        points.forEach((point, index) => index === 0 ? context.moveTo(point.x, point.y) : context.lineTo(point.x, point.y));
        context.closePath();
        context.fillStyle = fill;
        context.fill();
        context.strokeStyle = stroke;
        context.lineWidth = 1;
        context.stroke();
      };
      const tint = (color: string, factor: number) => {
        const value = Number.parseInt(color.slice(1), 16);
        const channels = [value >> 16, (value >> 8) & 255, value & 255].map((channel) => Math.round(channel + (255 - channel) * factor));
        return `rgb(${channels[0]}, ${channels[1]}, ${channels[2]})`;
      };

      const visible = placements.slice(0, visibleCount).sort((left, right) => {
        const leftDepth = left.position.x + left.position.z + left.position.y * 0.01;
        const rightDepth = right.position.x + right.position.z + right.position.y * 0.01;
        return leftDepth - rightDepth;
      });
      visible.forEach((placement) => {
        const { x, y, z } = placement.position;
        const length = placement.dimensions.length_cm;
        const itemWidth = placement.dimensions.width_cm;
        const itemHeight = placement.dimensions.height_cm;
        const p000 = project(x, y, z);
        const p100 = project(x + length, y, z);
        const p001 = project(x, y, z + itemWidth);
        const p101 = project(x + length, y, z + itemWidth);
        const p010 = project(x, y + itemHeight, z);
        const p110 = project(x + length, y + itemHeight, z);
        const p011 = project(x, y + itemHeight, z + itemWidth);
        const p111 = project(x + length, y + itemHeight, z + itemWidth);
        const color = packingColorForSKU(placement.warehouse_sku);
        const stroke = tint(color, -0.25);
        polygon([p000, p100, p110, p010], tint(color, 0.1), stroke);
        polygon([p000, p001, p011, p010], tint(color, 0.24), stroke);
        polygon([p010, p110, p111, p011], tint(color, 0.4), stroke);
        polygon([p100, p101, p111, p110], color, stroke);
        polygon([p001, p101, p111, p011], tint(color, 0.18), stroke);
      });

      const corners = [
        project(0, 0, 0), project(carton.length_cm, 0, 0), project(carton.length_cm, 0, carton.width_cm), project(0, 0, carton.width_cm),
        project(0, carton.height_cm, 0), project(carton.length_cm, carton.height_cm, 0), project(carton.length_cm, carton.height_cm, carton.width_cm), project(0, carton.height_cm, carton.width_cm),
      ];
      const edges = [[0, 1], [1, 2], [2, 3], [3, 0], [4, 5], [5, 6], [6, 7], [7, 4], [0, 4], [1, 5], [2, 6], [3, 7]];
      context.strokeStyle = "#56636a";
      context.lineWidth = 1.2;
      context.setLineDash([5, 4]);
      edges.forEach(([start, end]) => {
        context.beginPath();
        context.moveTo(corners[start].x, corners[start].y);
        context.lineTo(corners[end].x, corners[end].y);
        context.stroke();
      });
      context.setLineDash([]);
    };
    const resizeObserver = new ResizeObserver(draw);
    resizeObserver.observe(canvas);
    draw();
    return () => resizeObserver.disconnect();
  }, [carton, placements, visibleCount]);

  return <canvas ref={canvasRef} className="packing-canvas-2d" data-renderer="canvas2d" role="img" aria-label="装箱方案二维兼容视图" />;
}

export default function PackingScene(props: PackingSceneProps) {
  const [webGLFailed, setWebGLFailed] = useState(false);
  const useWebGL = props.rendererMode === "auto" && !webGLFailed && supportsWebGL2();
  const handleFailure = useCallback(() => setWebGLFailed(true), []);

  useEffect(() => {
    if (props.rendererMode === "auto") setWebGLFailed(false);
  }, [props.rendererMode]);

  return <div className="packing-scene" data-testid="packing-scene" data-active-renderer={useWebGL ? "webgl2" : "canvas2d"}>
    {useWebGL
      ? <WebGLScene {...props} onFailure={handleFailure} />
      : <CanvasScene carton={props.carton} placements={props.placements} visibleCount={props.visibleCount} />}
    <span className="packing-renderer-status">{useWebGL ? "3D · WebGL2" : "2D · 兼容模式"}</span>
  </div>;
}
