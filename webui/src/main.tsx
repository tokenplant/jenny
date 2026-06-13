import React from 'react';
import { render } from 'preact/compat';
import App from './App';

render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
  document.getElementById('root')!
);