// React state container with reducer pattern.
import { useState, useReducer } from 'react';

export interface State {
  count: number;
  name: string;
}

export type Action =
  | { type: 'INCREMENT'; amount: number }
  | { type: 'RENAME'; name: string };

export class StateContainer<T extends State> {
  private state: T;

  constructor(initial: T) {
    this.state = initial;
  }

  public update(action: Action): void {
    if (action.type === 'INCREMENT') {
      this.state = { ...this.state, count: this.state.count + action.amount };
    } else if (action.type === 'RENAME') {
      this.state = { ...this.state, name: action.name };
    }
  }

  public get(): T {
    return this.state;
  }

  public reset(initial: T): void {
    this.state = initial;
  }

  public snapshot(): string {
    return JSON.stringify(this.state);
  }
}

export function createContainer<T extends State>(initial: T): StateContainer<T> {
  return new StateContainer(initial);
}
