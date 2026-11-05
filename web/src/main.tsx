import { createRoot } from 'react-dom/client';
import App from './App';
import { PageBoundary } from './PageBoundary';
import './styles.css';

createRoot(document.getElementById('root')!).render(<PageBoundary name="omniHub"><App /></PageBoundary>);
