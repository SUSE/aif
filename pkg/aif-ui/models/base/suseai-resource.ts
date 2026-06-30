/**
 * @fileoverview Base resource class for SUSE AI extension
 * This module provides the foundation for all resource types in the SUSE AI extension,
 * following domain-driven design patterns for resource management.
 */

/**
 * Represents an action that can be performed on a resource
 * @interface Action
 */
export interface Action {
  /** Unique identifier for the action */
  action: string;
  /** Human-readable label for the action */
  label: string;
  /** Optional icon for the action */
  icon?: string;
  /** Whether this action can be performed on multiple resources */
  bulkable?: boolean;
  /** Whether the action is currently enabled */
  enabled?: boolean;
  /** Function to invoke when the action is triggered */
  invoke?: (opts: ActionOpts) => void | Promise<void>;
}

/**
 * Options passed to action invoke functions
 * @interface ActionOpts
 */
export interface ActionOpts {
  /** Vuex store instance */
  $store: any;
  /** Vue router instance */
  $router: any;
  /** Current route object */
  $route: any;
  /** The resource the action is being performed on */
  resource: any;
  /** Array of resources for bulk operations */
  resources?: any[];
  /** Target cluster ID for cluster-specific actions */
  cluster?: string;
}

/**
 * Represents the state of a resource
 * @interface ResourceState
 */
export interface ResourceState {
  /** Error message if the resource is in an error state */
  error?: string;
  /** Whether the resource is currently transitioning */
  transitioning?: boolean;
  /** General status message */
  message?: string;
}

/**
 * Metadata for Kubernetes-style resources
 * @interface ResourceMeta
 */
export interface ResourceMeta {
  /** Resource name */
  name?: string;
  /** Resource namespace */
  namespace?: string;
  /** Key-value labels */
  labels?: Record<string, string>;
  /** Key-value annotations */
  annotations?: Record<string, string>;
}

/**
 * Base class for SUSE AI resources
 *
 * This class provides common functionality for all resource types including:
 * - State management and computed properties
 * - Action handling and validation
 * - Store integration for data persistence
 * - Navigation and routing helpers
 *
 * @example
 * ```typescript
 * class AppResource extends SuseaiResource {
 *   get stateDisplay(): string {
 *     if (this.isInstalled) return 'Installed';
 *     return 'Available';
 *   }
 * }
 * ```
 */
export default class SuseaiResource {
  public metadata?: ResourceMeta;
  public status?: ResourceState;
  public spec?: any;
  
  protected $store?: any;
  protected $router?: any;
  protected $route?: any;

  /**
   * Creates a new SuseaiResource instance
   * @param data - Raw resource data to initialize the instance
   * @param store - Vuex store instance for state management
   * @param router - Vue router instance for navigation
   * @param route - Current route object
   */
  constructor(data: any = {}, store?: any, router?: any, route?: any) {
    Object.assign(this, data);
    this.$store = store;
    this.$router = router;
    this.$route = route;
  }

  /**
   * Get the unique identifier for this resource
   * @returns The resource name or empty string if not available
   */
  get id(): string {
    return this.metadata?.name || '';
  }

  /**
   * Get the display name of the resource
   * @returns The resource name or empty string if not available
   */
  get name(): string {
    return this.metadata?.name || '';
  }

  /**
   * Get the namespace of the resource
   * @returns The resource namespace or empty string if not available
   */
  get namespace(): string {
    return this.metadata?.namespace || '';
  }

  /**
   * Get the labels attached to this resource
   * @returns Object containing key-value label pairs
   */
  get labels(): Record<string, string> {
    return this.metadata?.labels || {};
  }

  /**
   * Get the annotations attached to this resource
   * @returns Object containing key-value annotation pairs
   */
  get annotations(): Record<string, string> {
    return this.metadata?.annotations || {};
  }

  /**
   * Check if the resource is in an error state
   * @returns True if the resource has an error, false otherwise
   */
  get hasError(): boolean {
    return !!this.status?.error;
  }

  /**
   * Check if the resource is currently transitioning
   * @returns True if the resource is transitioning, false otherwise
   */
  get isTransitioning(): boolean {
    return !!this.status?.transitioning;
  }

  /**
   * Get the current state message for this resource
   * @returns Status message, error message, or empty string
   */
  get stateMessage(): string {
    return this.status?.message || this.status?.error || '';
  }

  /**
   * Get human-readable state display text
   * Override this method in subclasses to provide resource-specific states
   * @returns State display string
   */
  get stateDisplay(): string {
    if (this.hasError) return 'Error';
    if (this.isTransitioning) return 'Transitioning';
    return 'Active';
  }

  /**
   * Get the color associated with the current state for UI display
   * @returns Color string ('error', 'info', 'success', etc.)
   */
  get stateColor(): string {
    if (this.hasError) return 'error';
    if (this.isTransitioning) return 'info';
    return 'success';
  }

  /**
   * Get the list of actions available for this resource
   * Override this method in subclasses to provide resource-specific actions
   * @returns Array of available actions
   */
  get availableActions(): Action[] {
    return [];
  }

  /**
   * Check if the resource can perform a specific action
   * @param actionName - Name of the action to check
   * @returns True if the action is available and enabled, false otherwise
   */
  canAction(actionName: string): boolean {
    return this.availableActions.some(action => action.action === actionName && action.enabled !== false);
  }

  /**
   * Perform an action on this resource
   * @param actionName - Name of the action to perform
   * @param opts - Additional options to pass to the action
   * @throws Error if the action is not available or not implemented
   */
  async performAction(actionName: string, opts: Partial<ActionOpts> = {}): Promise<void> {
    const action = this.availableActions.find(a => a.action === actionName);
    if (!action || !action.invoke) {
      throw new Error(`Action ${actionName} not available or not implemented`);
    }

    const actionOpts: ActionOpts = {
      $store: this.$store,
      $router: this.$router,
      $route: this.$route,
      resource: this,
      ...opts
    };

    await action.invoke(actionOpts);
  }

  /**
   * Dispatch an action to the Vuex store
   * @param action - Store action name
   * @param payload - Data to pass to the action
   * @returns Promise resolving to the action result
   * @throws Error if store is not available
   */
  protected async $dispatch(action: string, payload: any = {}): Promise<any> {
    if (!this.$store) {
      throw new Error('Store not available in resource');
    }
    return this.$store.dispatch(action, payload);
  }

  /**
   * Get a value from store getters
   * @param getter - Store getter name
   * @returns Value from the getter or null if store is not available
   */
  protected $get(getter: string): any {
    if (!this.$store) {
      return null;
    }
    return this.$store.getters[getter];
  }

  /**
   * Navigate to a route using Vue router
   * @param route - Route object or string to navigate to
   * @throws Error if router is not available
   */
  protected async $push(route: any): Promise<void> {
    if (!this.$router) {
      throw new Error('Router not available in resource');
    }
    await this.$router.push(route);
  }

  /**
   * Get the current route object
   * @returns Current route object
   */
  protected get $currentRoute(): any {
    return this.$route;
  }

  /**
   * Convert the resource to a plain JavaScript object
   * Excludes internal properties that start with '$'
   * @returns Plain object representation of the resource
   */
  toJSON(): any {
    const result: any = {};
    for (const [key, value] of Object.entries(this)) {
      if (!key.startsWith('$')) {
        result[key] = value;
      }
    }
    return result;
  }
}