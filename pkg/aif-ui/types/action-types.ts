/**
 * Action-related TypeScript interfaces and types
 * Provides comprehensive type definitions for action system
 */

// === Action Core Types ===

export type ActionContext = 
  | 'resource'
  | 'collection'
  | 'bulk'
  | 'global'
  | 'menu'
  | 'toolbar'
  | 'contextmenu';

export type ActionScope = 
  | 'single'
  | 'multiple'
  | 'all'
  | 'none';

export type ActionState = 
  | 'enabled'
  | 'disabled'
  | 'loading'
  | 'hidden';

export type ActionType = 
  | 'primary'
  | 'secondary'
  | 'danger'
  | 'warning'
  | 'info'
  | 'success'
  | 'default';

export type ConfirmationType = 
  | 'modal'
  | 'inline'
  | 'tooltip'
  | 'none';

// === Action Core Interfaces ===

export interface BaseAction {
  // Identity
  action: string;
  id?: string;
  
  // Display
  label: string;
  description?: string;
  tooltip?: string;
  icon?: string;
  iconSvg?: string;
  
  // Behavior
  enabled: boolean;
  loading?: boolean;
  hidden?: boolean;
  
  // Categorization
  type?: ActionType;
  category?: string;
  group?: string;
  priority?: number;
  
  // Context
  context?: ActionContext;
  scope?: ActionScope;
  
  // Visual styling
  variant?: 'button' | 'link' | 'icon' | 'dropdown';
  size?: 'xs' | 'sm' | 'md' | 'lg';
  color?: string;
  
  // Keyboard shortcut
  shortcut?: string;
  shortcutDescription?: string;
  
  // Permissions
  requiredPermissions?: string[];
  requiredCapabilities?: string[];
  
  // Conditions
  showWhen?: (context: ActionExecutionContext) => boolean;
  enabledWhen?: (context: ActionExecutionContext) => boolean;
  
  // Metadata
  tags?: string[];
  metadata?: Record<string, any>;
}

export interface ResourceAction extends BaseAction {
  // Execution
  invoke: (opts: ActionOpts) => Promise<void> | void;
  
  // Bulk operation support
  bulkAction?: boolean;
  bulkExecute?: (resources: any[], opts: ActionOpts) => Promise<void> | void;
  
  // Resource-specific
  resourceTypes?: string[];
  resourceStates?: string[];
  
  // Confirmation
  requiresConfirmation?: boolean;
  confirmationMessage?: string | ((opts: ActionOpts) => string);
  confirmationType?: ConfirmationType;
  
  // Error handling
  onError?: (error: Error, opts: ActionOpts) => void;
  onSuccess?: (result: any, opts: ActionOpts) => void;
  
  // Loading state
  loadingMessage?: string;
  successMessage?: string;
  errorMessage?: string;
}

export interface MenuAction extends BaseAction {
  // Execution
  execute: (context: ActionExecutionContext) => Promise<void> | void;
  
  // Menu-specific
  separator?: boolean;
  divider?: boolean;
  submenu?: MenuAction[];
  
  // Navigation
  route?: string | RouteTarget;
  external?: boolean;
  target?: '_blank' | '_self' | '_parent' | '_top';
  
  // Dynamic properties
  dynamicLabel?: (context: ActionExecutionContext) => string;
  dynamicIcon?: (context: ActionExecutionContext) => string;
  dynamicTooltip?: (context: ActionExecutionContext) => string;
}

export interface BulkAction extends BaseAction {
  // Bulk execution
  execute: (items: any[], context: ActionExecutionContext) => Promise<ActionResult[]>;
  
  // Bulk-specific
  minItems?: number;
  maxItems?: number;
  
  // Confirmation for bulk operations
  bulkConfirmationMessage?: string | ((count: number) => string);
  bulkConfirmationType?: ConfirmationType;
  
  // Progress tracking
  showProgress?: boolean;
  progressTitle?: string;
  
  // Error handling
  continueOnError?: boolean;
  reportPartialSuccess?: boolean;
}

export interface WorkflowAction extends BaseAction {
  // Workflow steps
  steps: WorkflowStep[];
  
  // Workflow execution
  execute: (context: ActionExecutionContext) => Promise<WorkflowResult>;
  
  // Workflow control
  canCancel?: boolean;
  canSkipSteps?: boolean;
  canRetryFailed?: boolean;
  
  // Progress
  showStepProgress?: boolean;
  showOverallProgress?: boolean;
}

// === Action Execution Context ===

export interface ActionExecutionContext {
  // Resource context
  resource?: any;
  resources?: any[];
  
  // UI context
  component?: any;
  router?: any;
  route?: any;
  store?: any;
  
  // User context
  user?: any;
  permissions?: string[];
  
  // Cluster context
  cluster?: any;
  clusterId?: string;
  namespace?: string;
  
  // Additional context
  metadata?: Record<string, any>;
  
  // Event information
  event?: Event;
  source?: string;
}

export interface ActionOpts {
  // Resource
  resource: any;
  resources?: any[];
  
  // Context
  alt?: boolean;
  ctrl?: boolean;
  shift?: boolean;
  meta?: boolean;
  
  // UI references
  $router?: any;
  $route?: any;
  $store?: any;
  
  // Additional options
  [key: string]: any;
}

// === Action Results ===

export interface ActionResult {
  success: boolean;
  message?: string;
  data?: any;
  error?: Error | string;
  
  // Resource information
  resource?: any;
  resourceId?: string;
  
  // Timing
  duration?: number; // in milliseconds
  timestamp?: string;
  
  // Additional metadata
  metadata?: Record<string, any>;
}

export interface WorkflowResult {
  success: boolean;
  completedSteps: number;
  totalSteps: number;
  results: WorkflowStepResult[];
  
  // Overall information
  startTime: string;
  endTime?: string;
  duration?: number; // in milliseconds
  
  // Error information
  error?: Error | string;
  failedStepIndex?: number;
  
  // Metadata
  metadata?: Record<string, any>;
}

export interface WorkflowStep {
  id: string;
  name: string;
  description?: string;
  
  // Execution
  execute: (context: ActionExecutionContext) => Promise<WorkflowStepResult>;
  
  // Flow control
  optional?: boolean;
  retryable?: boolean;
  retryCount?: number;
  
  // Conditions
  condition?: (context: ActionExecutionContext) => boolean | Promise<boolean>;
  
  // Dependencies
  dependsOn?: string[];
  
  // Timeout
  timeout?: number; // in milliseconds
  
  // UI
  showProgress?: boolean;
  progressMessage?: string;
}

export interface WorkflowStepResult {
  stepId: string;
  success: boolean;
  message?: string;
  data?: any;
  error?: Error | string;
  
  // Timing
  startTime: string;
  endTime: string;
  duration: number; // in milliseconds
  
  // Retry information
  retryCount: number;
  
  // Metadata
  metadata?: Record<string, any>;
}

// === Action Configuration ===

export interface ActionConfiguration {
  // Global settings
  enabled: boolean;
  showTooltips: boolean;
  showShortcuts: boolean;
  showIcons: boolean;
  
  // Confirmation settings
  requireConfirmation: ConfirmationType;
  confirmationTimeout: number; // in milliseconds
  
  // Execution settings
  defaultTimeout: number; // in milliseconds
  maxRetries: number;
  showProgress: boolean;
  
  // Bulk operation settings
  maxBulkItems: number;
  bulkBatchSize: number;
  bulkDelay: number; // delay between batch operations in milliseconds
  
  // Error handling
  showErrorDetails: boolean;
  logErrors: boolean;
  
  // Performance
  debounceTime: number; // in milliseconds
  
  // Customization
  customActions: ActionDefinition[];
  disabledActions: string[];
  actionOrder: string[];
  
  // Keyboard shortcuts
  shortcuts: Record<string, string>; // shortcut -> action id
}

export interface ActionDefinition {
  id: string;
  type: 'resource' | 'menu' | 'bulk' | 'workflow';
  config: any; // Configuration specific to action type
  
  // Override default behavior
  overrides?: {
    label?: string;
    icon?: string;
    enabled?: boolean;
    hidden?: boolean;
  };
}

// === Action Registry ===

export interface ActionRegistry {
  // Registration
  register(action: ResourceAction | MenuAction | BulkAction | WorkflowAction): void;
  unregister(actionId: string): void;
  
  // Retrieval
  get(actionId: string): BaseAction | undefined;
  getAll(): BaseAction[];
  getByContext(context: ActionContext): BaseAction[];
  getByResourceType(resourceType: string): ResourceAction[];
  getByCategory(category: string): BaseAction[];
  
  // Filtering
  filter(predicate: (action: BaseAction) => boolean): BaseAction[];
  search(query: string): BaseAction[];
  
  // State management
  isEnabled(actionId: string, context?: ActionExecutionContext): boolean;
  isVisible(actionId: string, context?: ActionExecutionContext): boolean;
  canExecute(actionId: string, context?: ActionExecutionContext): boolean;
  
  // Execution
  execute(actionId: string, context: ActionExecutionContext): Promise<ActionResult>;
  executeBulk(actionId: string, items: any[], context: ActionExecutionContext): Promise<ActionResult[]>;
}

// === Action Manager ===

export interface ActionManager {
  // Registry access
  registry: ActionRegistry;
  
  // Configuration
  config: ActionConfiguration;
  
  // Execution tracking
  activeActions: Map<string, ActionExecution>;
  actionHistory: ActionHistoryEntry[];
  
  // Methods
  executeAction(action: BaseAction, context: ActionExecutionContext): Promise<ActionResult>;
  executeBulkAction(action: BulkAction, items: any[], context: ActionExecutionContext): Promise<ActionResult[]>;
  executeWorkflow(action: WorkflowAction, context: ActionExecutionContext): Promise<WorkflowResult>;
  
  // State management
  getActionState(actionId: string, context?: ActionExecutionContext): ActionState;
  setActionState(actionId: string, state: ActionState): void;
  
  // Event system
  on(event: ActionEvent, handler: ActionEventHandler): void;
  off(event: ActionEvent, handler: ActionEventHandler): void;
  emit(event: ActionEvent, data: any): void;
  
  // Utilities
  hasPermission(action: BaseAction, context: ActionExecutionContext): boolean;
  validateAction(action: BaseAction): ActionValidationResult;
}

export interface ActionExecution {
  id: string;
  actionId: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  progress: number; // 0-100
  
  // Context
  context: ActionExecutionContext;
  
  // Timing
  startTime: string;
  endTime?: string;
  duration?: number; // in milliseconds
  
  // Result
  result?: ActionResult | ActionResult[] | WorkflowResult;
  error?: Error | string;
  
  // Control
  cancellable: boolean;
  cancel?: () => void;
}

export interface ActionHistoryEntry {
  id: string;
  actionId: string;
  actionLabel: string;
  
  // Context
  resourceType?: string;
  resourceId?: string;
  clusterId?: string;
  namespace?: string;
  
  // Result
  success: boolean;
  message?: string;
  error?: string;
  
  // Timing
  timestamp: string;
  duration: number; // in milliseconds
  
  // User
  user?: string;
}

// === Action Events ===

export type ActionEvent = 
  | 'action:registered'
  | 'action:unregistered'
  | 'action:executed'
  | 'action:started'
  | 'action:completed'
  | 'action:failed'
  | 'action:cancelled'
  | 'action:progress'
  | 'bulk:started'
  | 'bulk:progress'
  | 'bulk:completed'
  | 'workflow:started'
  | 'workflow:step:started'
  | 'workflow:step:completed'
  | 'workflow:step:failed'
  | 'workflow:completed';

export interface ActionEventData {
  actionId: string;
  executionId?: string;
  context?: ActionExecutionContext;
  result?: any;
  error?: Error | string;
  progress?: number;
  metadata?: Record<string, any>;
}

export type ActionEventHandler = (data: ActionEventData) => void;

// === Action Validation ===

export interface ActionValidationResult {
  valid: boolean;
  errors: ActionValidationError[];
  warnings: ActionValidationWarning[];
}

export interface ActionValidationError {
  field: string;
  message: string;
  code: string;
}

export interface ActionValidationWarning {
  field: string;
  message: string;
  suggestion?: string;
}

// === Action UI Components ===

export interface ActionButton {
  action: BaseAction;
  size?: 'xs' | 'sm' | 'md' | 'lg';
  variant?: 'primary' | 'secondary' | 'ghost' | 'link';
  fullWidth?: boolean;
  loading?: boolean;
  disabled?: boolean;
  
  // Event handlers
  onClick?: (action: BaseAction, context: ActionExecutionContext) => void;
}

export interface ActionDropdown {
  actions: BaseAction[];
  label?: string;
  icon?: string;
  size?: 'xs' | 'sm' | 'md' | 'lg';
  variant?: 'primary' | 'secondary' | 'ghost';
  
  // Grouping
  grouped?: boolean;
  groupBy?: (action: BaseAction) => string;
  
  // Filtering
  filter?: (action: BaseAction, context: ActionExecutionContext) => boolean;
  
  // Event handlers
  onSelect?: (action: BaseAction, context: ActionExecutionContext) => void;
}

export interface ActionMenu {
  actions: MenuAction[];
  context?: ActionExecutionContext;
  
  // Appearance
  showIcons?: boolean;
  showShortcuts?: boolean;
  showTooltips?: boolean;
  
  // Behavior
  closeOnAction?: boolean;
  preventClose?: string[]; // action IDs that don't close menu
  
  // Event handlers
  onAction?: (action: MenuAction, context: ActionExecutionContext) => void;
}

export interface ActionToolbar {
  actions: BaseAction[];
  context?: ActionExecutionContext;
  
  // Layout
  layout?: 'horizontal' | 'vertical';
  wrap?: boolean;
  spacing?: 'xs' | 'sm' | 'md' | 'lg';
  
  // Grouping
  grouped?: boolean;
  separator?: boolean;
  
  // Overflow handling
  overflowMode?: 'wrap' | 'scroll' | 'dropdown';
  maxVisible?: number;
  
  // Event handlers
  onAction?: (action: BaseAction, context: ActionExecutionContext) => void;
}

// === Navigation Types ===

export interface RouteTarget {
  name?: string;
  path?: string;
  params?: Record<string, any>;
  query?: Record<string, any>;
  hash?: string;
  
  // Modifiers
  replace?: boolean;
  append?: boolean;
}

// === Utility Types ===

export type ActionId = string;
export type ActionGroupId = string;
export type ActionCategoryId = string;

// === Type Guards ===

export function isResourceAction(action: BaseAction): action is ResourceAction {
  return 'invoke' in action && typeof (action as any).invoke === 'function';
}

export function isMenuAction(action: BaseAction): action is MenuAction {
  return 'execute' in action && typeof (action as any).execute === 'function';
}

export function isBulkAction(action: BaseAction): action is BulkAction {
  return action.scope === 'multiple' && 'execute' in action;
}

export function isWorkflowAction(action: BaseAction): action is WorkflowAction {
  return 'steps' in action && Array.isArray((action as any).steps);
}

export function hasSubmenu(action: MenuAction): action is MenuAction & { submenu: MenuAction[] } {
  return !!action.submenu && action.submenu.length > 0;
}

export function requiresConfirmation(action: ResourceAction): boolean {
  return !!action.requiresConfirmation;
}

export function isActionEnabled(action: BaseAction, context?: ActionExecutionContext): boolean {
  if (!action.enabled) return false;
  if (action.enabledWhen && context) return action.enabledWhen(context);
  return true;
}

export function isActionVisible(action: BaseAction, context?: ActionExecutionContext): boolean {
  if (action.hidden) return false;
  if (action.showWhen && context) return action.showWhen(context);
  return true;
}

export function canExecuteAction(action: BaseAction, context?: ActionExecutionContext): boolean {
  return isActionEnabled(action, context) && isActionVisible(action, context);
}